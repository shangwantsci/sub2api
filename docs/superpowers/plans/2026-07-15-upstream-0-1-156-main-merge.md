# upstream 0.1.156 合并实施计划

设计：`docs/superpowers/specs/2026-07-15-upstream-0-1-156-main-merge-design.md`

冻结 ref：`upstream/main = be6cd1250c10ba2812305dcf071da133e4d738f6`
基线：`custom/prod = e95abe6cb`
工作树：`.worktrees/upstream-0.1.156-main-merge`，分支 `merge/upstream-0.1.156-main`

## Task 0：基线（已完成）

- [x] 创建隔离 worktree 与分支
- [x] merge-tree 预演，确认 9 冲突
- [x] 风险探测（快照版本无撞车、fork 核心文件上游零 diff、migration 仅 177）
- [x] 基线报告 `.superpowers/sdd/upstream-156-task-0-baseline.md`

## Task 1：文档提交 + 合并 + 冲突解决

- [ ] 提交设计与计划文档到合并分支
- [ ] `git merge --no-ff --no-commit be6cd125`
- [ ] 按设计 §3.2 逐一解决 9 个冲突（禁 --ours/--theirs）
- [ ] `go build ./...` 编译通过
- [ ] `go generate ./ent` 幂等（无非预期 diff）
- [ ] `git diff --check` 干净
- [ ] 提交 merge commit（第二父 = be6cd125）
- 闸门 1：冲突解决逐项对照设计复查；VERSION=0.1.156；报告 `.superpowers/sdd/upstream-156-task-1-merge.md`

## Task 2：后端验证

- [ ] 不变量锚点检索（2.1.206、Fable、count_tokens、TLS、mixed-pool、Bedrock、sessionKey 导入、pool-health、v16、fast policy 原子写）
- [ ] fork 核心文件与 custom/prod 零 diff 验证
- [ ] focused tests：failover_loop、gateway handler、account_handler/duplicate、grok、agent identity、apicompat、scheduler
- [ ] `go test -tags=unit ./... -count=1` 全量
- 闸门 2：报告 `.superpowers/sdd/upstream-156-task-2-backend.md`

## Task 3：前端验证

- [ ] focused specs：OAuthAuthorizationFlow、CreateAccountModal、SettingsView、credentialsBuilder、locales
- [ ] `pnpm test:run` 全量
- [ ] `pnpm build`
- 闸门 3：报告 `.superpowers/sdd/upstream-156-task-3-frontend.md`

## Task 4：语义审查 + 独立审查

- [ ] 51 个双方共改的自动合并文件按设计 §4 P0 清单复核
- [ ] migration 目录：仅新增 177，无改写，154 保留
- [ ] 固定 diff 包交独立审查代理整分支审查；Critical/Important 修复并复审
- 闸门 4：报告 `.superpowers/sdd/upstream-156-task-4-review.md`

## Task 5：交付

- [ ] 合并回 `custom/prod`（fast-forward 或按用户选择）
- [ ] 清理 worktree
- [ ] 最终报告；推送与部署按用户指示执行
