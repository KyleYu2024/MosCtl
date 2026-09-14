# MosDNS 内核自动同步

`MosCtl` 使用 GitHub Actions 定时检查 `irinesistiana/mosdns:latest` 的 `linux/amd64` manifest digest。

当 digest 变化时，工作流会：

1. 检出 `docker` 分支。
2. 使用新 digest 锁定 MosDNS 构建来源。
3. 运行 Go 测试和 `go vet`。
4. 构建 `linux/amd64` 候选镜像，验证 MosDNS 与 MosCtl 二进制可执行。
5. 推送 `ghcr.io/kyleyu2024/mosctl:latest` 和 `ghcr.io/kyleyu2024/mosctl:mosdns-<digest前12位>`。
6. 仅在镜像推送成功后，更新 `.github/mosdns-amd64.digest` 并提交到 `docker` 分支。

## GitHub 配置

镜像发布到 GitHub Container Registry（GHCR）。工作流使用仓库自带的 `GITHUB_TOKEN` 登录 `ghcr.io`，权限限定为 `contents: write` 和 `packages: write`，不需要配置 Docker Hub 密钥。

仓库为公开仓库时，由该工作流创建并关联的 GHCR 包可设为 Public，NAS 便可不登录直接拉取。

## 触发方式

- 自动：每 6 小时的第 17 分钟检查一次。
- 手动：在 Actions 页面运行 `Sync MosDNS kernel`。
- 手动强制重建：运行时将 `force` 设为 `true`。

GitHub 的定时工作流只从默认分支调度，因此 `sync-mosdns.yml` 必须保留在默认的 `main` 分支。工作流内部会明确检出 `docker` 分支进行构建和状态提交。

## NAS 更新

该工作流只负责生成并推送通过测试的 GHCR 镜像，不会直接重建 NAS 上的 DNS 容器。这样可以避免上游不兼容更新立即中断局域网 DNS。

如需自动更新 NAS，可额外使用只限 `mosctl` 容器的 Watchtower 标签；建议先使用 monitor-only 模式观察。
