// Package kernel manages verified MosDNS binary updates and rollback.
package kernel

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const releaseAPI = "https://api.github.com/repos/IrineSistiana/mosdns/releases/latest"
const maxDownload = 64 << 20

var versionPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-0-g[0-9a-f]+)?$`)

type Status struct {
	Current         string `json:"current"`
	Latest          string `json:"latest,omitempty"`
	Backup          string `json:"backup,omitempty"`
	Busy            bool   `json:"busy"`
	Phase           string `json:"phase"`
	Message         string `json:"message"`
	Error           string `json:"error,omitempty"`
	Supported       bool   `json:"supported"`
	UpdateAvailable bool   `json:"update_available"`
}
type asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}
type release struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []asset `json:"assets"`
}
type Options struct {
	Binary    string
	Supported bool
	Restart   func(context.Context) error
	Health    func(context.Context) error
	Client    *http.Client
	Version   func(context.Context, string) (string, error)
}
type Manager struct {
	mu       sync.Mutex
	opts     Options
	status   Status
	selected asset
	tag      string
}

func New(opts Options) *Manager {
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: 90 * time.Second}
	}
	if opts.Version == nil {
		opts.Version = binaryVersion
	}
	m := &Manager{opts: opts, status: Status{Supported: opts.Supported, Phase: "idle"}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.status.Current, _ = opts.Version(ctx, opts.Binary)
	m.status.Backup, _ = opts.Version(ctx, opts.Binary+".mosctl-backup")
	if m.status.Current == "" {
		m.status.Supported = false
		m.status.Message = "无法读取当前 MosDNS 版本"
	}
	if !opts.Supported {
		m.status.Message = "内核更新仅支持 Linux 原生进程管理部署；Docker 请更新镜像"
	}
	return m
}
func binaryVersion(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "version")
	var out limitedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("读取版本失败: %w", err)
	}
	v := strings.TrimSpace(out.String())
	if !versionPattern.MatchString(v) {
		return "", fmt.Errorf("无法识别内核版本 %q", v)
	}
	return v, nil
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < 4096 {
		keep := 4096 - b.Len()
		if len(p) > keep {
			p = p[:keep]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}
func versionNumbers(s string) ([3]int, bool) {
	var v [3]int
	parts := versionPattern.FindStringSubmatch(s)
	if parts == nil {
		return v, false
	}
	for i := range v {
		n, e := strconv.Atoi(parts[i+1])
		if e != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}
func newer(a, b string) bool {
	av, ok := versionNumbers(a)
	bv, ok2 := versionNumbers(b)
	if !ok || !ok2 {
		return false
	}
	for i := range av {
		if av[i] != bv[i] {
			return av[i] > bv[i]
		}
	}
	return false
}
func (m *Manager) Snapshot() Status { m.mu.Lock(); defer m.mu.Unlock(); return m.status }
func (m *Manager) set(phase, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.Phase = phase
	m.status.Message = message
}

// Start reserves the operation before launching background work; concurrent requests are rejected.
func (m *Manager) Start(ctx context.Context, action string) error {
	m.mu.Lock()
	if m.status.Busy {
		m.mu.Unlock()
		return errors.New("已有内核操作正在进行")
	}
	if !m.status.Supported {
		m.mu.Unlock()
		return errors.New("当前部署不支持内核更新")
	}
	switch action {
	case "check":
	case "update":
		if !m.status.UpdateAvailable || m.selected.URL == "" {
			m.mu.Unlock()
			return errors.New("请先检查可用更新")
		}
	case "rollback":
		if m.status.Backup == "" {
			m.mu.Unlock()
			return errors.New("没有可回滚的内核")
		}
	default:
		m.mu.Unlock()
		return errors.New("操作无效")
	}
	m.status.Busy = true
	m.status.Error = ""
	m.status.Phase = action
	m.status.Message = "正在处理…"
	m.mu.Unlock()
	go func() {
		taskCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		defer cancel()
		var err error
		if action == "check" {
			err = m.check(taskCtx)
		} else if action == "update" {
			err = m.update(taskCtx)
		} else {
			err = m.rollback(taskCtx)
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		m.status.Busy = false
		if err != nil {
			m.status.Phase = "error"
			m.status.Error = err.Error()
			m.status.Message = err.Error()
		} else {
			m.status.Phase = "done"
		}
	}()
	return nil
}
func (m *Manager) get(ctx context.Context, target string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MosCtl-kernel-updater")
	if target == releaseAPI {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	resp, err := m.opts.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("下载返回 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("下载文件超出大小限制")
	}
	return data, nil
}
func trustedAsset(a asset, tag string) bool {
	u, err := url.Parse(a.URL)
	if err != nil {
		return false
	}
	digest, err := hex.DecodeString(strings.TrimPrefix(a.Digest, "sha256:"))
	return err == nil && len(digest) == 32 && strings.HasPrefix(a.Digest, "sha256:") && u.Scheme == "https" && u.Host == "github.com" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == "/IrineSistiana/mosdns/releases/download/"+tag+"/"+a.Name
}
func (m *Manager) check(ctx context.Context) error {
	m.set("checking", "正在检查官方稳定版本…")
	data, err := m.get(ctx, releaseAPI, 2<<20)
	if err != nil {
		return fmt.Errorf("检查更新失败: %w", err)
	}
	var r release
	if err = json.Unmarshal(data, &r); err != nil {
		return errors.New("官方版本信息无效")
	}
	if _, ok := versionNumbers(r.Tag); !ok || r.Draft || r.Prerelease {
		return errors.New("未找到有效稳定版本")
	}
	name := "mosdns-linux-" + runtime.GOARCH + ".zip"
	var selected asset
	for _, a := range r.Assets {
		if a.Name == name && trustedAsset(a, r.Tag) {
			selected = a
			break
		}
	}
	if selected.URL == "" {
		return errors.New("没有当前架构的可校验安装包")
	}
	current, err := m.opts.Version(ctx, m.opts.Binary)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tag = r.Tag
	m.selected = selected
	m.status.Current = current
	m.status.Latest = r.Tag
	m.status.UpdateAvailable = newer(r.Tag, current)
	m.status.Message = "当前已是最新稳定版本"
	if m.status.UpdateAvailable {
		m.status.Message = "发现新版本 " + r.Tag
	}
	return nil
}
func extractBinary(data []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("安装包不是有效 ZIP")
	}
	var binary []byte
	for _, f := range zr.File {
		if f.Name != "mosdns" && f.Name != "./mosdns" {
			continue
		}
		if binary != nil || !f.Mode().IsRegular() || f.UncompressedSize64 > maxDownload {
			return nil, errors.New("内核文件无效")
		}
		rc, e := f.Open()
		if e != nil {
			return nil, e
		}
		binary, e = io.ReadAll(io.LimitReader(rc, maxDownload+1))
		rc.Close()
		if e != nil {
			return nil, e
		}
		if len(binary) > maxDownload {
			return nil, errors.New("内核文件过大")
		}
	}
	if len(binary) < 4 || !bytes.Equal(binary[:4], []byte{0x7f, 'E', 'L', 'F'}) {
		return nil, errors.New("安装包缺少 Linux 内核")
	}
	return binary, nil
}
func stage(path string, data []byte) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".mosdns-stage-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(name)
		}
	}()
	if err = f.Chmod(0755); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return name, err
}
func syncDir(path string) error {
	f, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func atomicCopy(source, target string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	name, err := stage(target, data)
	if err != nil {
		return err
	}
	defer os.Remove(name)
	if err = os.Rename(name, target); err != nil {
		return err
	}
	return syncDir(target)
}

// Recover restores an unfinished transaction before the supervisor starts MosDNS.
func Recover(binary string) error {
	pending := binary + ".mosctl-pending"
	if _, err := os.Stat(pending); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if err := os.Rename(pending, binary); err != nil {
		return err
	}
	return syncDir(binary)
}
func (m *Manager) update(ctx context.Context) error {
	m.mu.Lock()
	a, tag := m.selected, m.tag
	m.mu.Unlock()
	m.set("downloading", "正在下载 "+tag+"…")
	data, err := m.get(ctx, a.URL, maxDownload)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != strings.TrimPrefix(a.Digest, "sha256:") {
		return errors.New("SHA-256 校验失败，未修改当前内核")
	}
	binary, err := extractBinary(data)
	if err != nil {
		return err
	}
	name, err := stage(m.opts.Binary, binary)
	if err != nil {
		return err
	}
	defer os.Remove(name)
	m.set("verifying", "正在验证内核版本…")
	v, err := m.opts.Version(ctx, name)
	if err != nil {
		return err
	}
	av, _ := versionNumbers(v)
	bv, _ := versionNumbers(tag)
	if av != bv {
		return errors.New("安装包版本与发布版本不一致")
	}
	if err = m.install(ctx, name); err != nil {
		return err
	}
	m.set("done", "MosDNS 已更新到 "+v)
	return nil
}
func (m *Manager) rollback(ctx context.Context) error {
	m.set("verifying", "正在验证备份内核…")
	backup := m.opts.Binary + ".mosctl-backup"
	data, err := os.ReadFile(backup)
	if err != nil {
		return err
	}
	name, err := stage(m.opts.Binary, data)
	if err != nil {
		return err
	}
	defer os.Remove(name)
	v, err := m.opts.Version(ctx, name)
	if err != nil {
		return err
	}
	if err = m.install(ctx, name); err != nil {
		return err
	}
	m.set("done", "已回滚到 "+v)
	return nil
}
func (m *Manager) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	current, _ := m.opts.Version(ctx, m.opts.Binary)
	backup, _ := m.opts.Version(ctx, m.opts.Binary+".mosctl-backup")
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.Current = current
	m.status.Backup = backup
	m.status.UpdateAvailable = newer(m.status.Latest, current)
}
func (m *Manager) install(ctx context.Context, candidate string) error {
	defer m.refresh()
	pending := m.opts.Binary + ".mosctl-pending"
	if _, err := os.Stat(pending); err == nil {
		return errors.New("存在未完成的内核操作，请重启 MosCtl 恢复")
	}
	m.set("installing", "正在备份并替换内核…")
	if err := atomicCopy(m.opts.Binary, pending); err != nil {
		return fmt.Errorf("备份失败: %w", err)
	}
	if err := os.Rename(candidate, m.opts.Binary); err != nil {
		os.Remove(pending)
		return fmt.Errorf("替换失败: %w", err)
	}
	err := syncDir(m.opts.Binary)
	if err == nil {
		m.set("restarting", "正在重启并检查 DNS…")
		err = m.opts.Restart(ctx)
	}
	if err == nil {
		err = m.opts.Health(ctx)
	}
	if err != nil {
		m.set("recovering", "更新检查未通过，正在恢复旧内核…")
		if e := Recover(m.opts.Binary); e != nil {
			return fmt.Errorf("检查失败 (%v)，恢复旧内核失败: %w", err, e)
		}
		recoveryCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if e := m.opts.Restart(recoveryCtx); e != nil {
			return fmt.Errorf("旧内核已恢复，但重启失败: %w", e)
		}
		if e := m.opts.Health(recoveryCtx); e != nil {
			return fmt.Errorf("旧内核已恢复，但 DNS 检查未通过: %w", e)
		}
		return fmt.Errorf("更新检查失败，已恢复旧内核: %w", err)
	}
	if err := os.Rename(pending, m.opts.Binary+".mosctl-backup"); err != nil {
		return fmt.Errorf("内核已运行，但备份归档失败，请重启恢复: %w", err)
	}
	return syncDir(m.opts.Binary)
}
