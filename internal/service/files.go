package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// OperationMu serializes configuration application, manual restarts and kernel installation.
var OperationMu sync.Mutex

var managedFiles = struct {
	sync.Mutex
	hashes map[string][32]byte
}{hashes: make(map[string][32]byte)}

type stagedWriter interface {
	Chmod(os.FileMode) error
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

func writeStaged(f stagedWriter, data []byte, mode os.FileMode) error {
	err := f.Chmod(mode)
	if err == nil {
		var n int
		n, err = f.Write(data)
		if err == nil && n != len(data) {
			err = io.ErrShortWrite
		}
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

// AtomicWrite never replaces the destination when staging fails.
func AtomicWrite(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".mosctl-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = writeStaged(f, data, mode); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// WriteManaged suppresses only watcher events whose current contents match this managed write.
// An external edit with different contents is still reloaded, even immediately afterwards.
func WriteManaged(path string, data []byte, mode os.FileMode) error {
	managedFiles.Lock()
	defer managedFiles.Unlock()
	if err := AtomicWrite(path, data, mode); err != nil {
		return err
	}
	managedFiles.hashes[path] = sha256.Sum256(data)
	return nil
}

func ManagedContents(path string) bool {
	managedFiles.Lock()
	defer managedFiles.Unlock()
	hash, ok := managedFiles.hashes[path]
	if !ok {
		return false
	}
	data, err := os.ReadFile(path)
	return err == nil && hash == sha256.Sum256(data)
}

type FileBackup struct {
	Format      int         `json:"format"`
	Transaction string      `json:"transaction,omitempty"`
	CommitPath  string      `json:"commit_path,omitempty"`
	Existed     bool        `json:"existed"`
	Data        []byte      `json:"data"`
	Mode        os.FileMode `json:"mode"`
}

func BackupFile(path string) (FileBackup, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return FileBackup{Mode: 0644}, nil
	}
	if err != nil {
		return FileBackup{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return FileBackup{}, err
	}
	return FileBackup{Existed: true, Data: data, Mode: info.Mode().Perm()}, nil
}

func (b FileBackup) Restore(path string) error {
	if b.Existed {
		return WriteManaged(path, b.Data, b.Mode)
	}
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (b FileBackup) Journal(path string) error {
	b.Format = 1
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return AtomicWrite(path+".mosctl-pending", data, 0600)
}

// RecoverFile restores an interrupted Web application before MosDNS starts.
func RecoverFile(path string) error {
	journal := path + ".mosctl-pending"
	data, err := os.ReadFile(journal)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var b FileBackup
	if err = json.Unmarshal(data, &b); err != nil {
		return fmt.Errorf("无效恢复记录 %s: %w", path, err)
	}
	if b.Format != 1 || b.Mode == 0 {
		return fmt.Errorf("恢复记录格式无效，未修改 %s", path)
	}
	committed := false
	if b.Transaction != "" && b.CommitPath != "" {
		marker, _ := os.ReadFile(b.CommitPath)
		committed = string(marker) == b.Transaction
	}
	if !committed {
		if err = b.Restore(path); err != nil {
			return err
		}
	}
	return os.Remove(journal)
}

// ApplyFiles commits a validated batch once, verifies DNS, and restores the whole batch on failure.
// Caller holds OperationMu so kernel updates and other applications cannot overlap.
func ApplyFiles(ctx context.Context, files map[string][]byte, apply func(context.Context) error) error {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil
	}
	for _, path := range paths {
		if _, err := os.Stat(path + ".mosctl-pending"); err == nil {
			return fmt.Errorf("存在未完成的配置应用，请重启 MosCtl 恢复")
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	transaction := hex.EncodeToString(nonce)
	commitPath := paths[0] + ".mosctl-committed"
	backups := make(map[string]FileBackup)
	cleanup := func() error {
		for _, path := range paths {
			if _, ok := backups[path]; ok {
				if err := os.Remove(path + ".mosctl-pending"); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
		}
		if err := os.Remove(commitPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	for _, path := range paths {
		b, err := BackupFile(path)
		if err != nil {
			_ = cleanup()
			return fmt.Errorf("读取原文件失败: %w", err)
		}
		b.Transaction = transaction
		b.CommitPath = commitPath
		if err = b.Journal(path); err != nil {
			_ = cleanup()
			return fmt.Errorf("创建恢复记录失败: %w", err)
		}
		backups[path] = b
	}
	var failure error
	for _, path := range paths {
		if err := WriteManaged(path, files[path], backups[path].Mode); err != nil {
			failure = fmt.Errorf("文件写入失败: %w", err)
			break
		}
	}
	if failure == nil {
		applyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		failure = apply(applyCtx)
		cancel()
	}
	if failure == nil {
		failure = AtomicWrite(commitPath, []byte(transaction), 0600)
	}
	if failure == nil {
		if err := cleanup(); err != nil {
			return fmt.Errorf("DNS 已通过检查，但恢复记录清理失败: %w", err)
		}
		return nil
	}
	for _, path := range paths {
		if err := backups[path].Restore(path); err != nil {
			return fmt.Errorf("应用失败 (%v)，恢复原文件失败: %w", failure, err)
		}
	}
	recoveryCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := apply(recoveryCtx); err != nil {
		return fmt.Errorf("原文件已恢复，但 DNS 恢复检查失败: %w", err)
	}
	if err := cleanup(); err != nil {
		return fmt.Errorf("原文件已恢复，恢复记录清理失败: %w", err)
	}
	return fmt.Errorf("应用失败，已恢复原文件并通过 DNS 检查: %w", failure)
}
