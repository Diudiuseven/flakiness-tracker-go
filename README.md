# flakiness-tracker-go

测试不稳定度跟踪与可靠性判定服务。它将多次运行的用例结果长期汇总为可查询的
稳定性画像，并据此完成**用例隔离（quarantine）**与**运行放行（gate）**的工程决策。

## 架构

```
cmd/server            HTTP JSON API 入口
internal/model        核心类型：Batch / CaseResult / Status / RejectError
internal/store        并发安全的内存仓库：原子批次写入、去重、按 run_index 排序、保留回收
internal/flakiness    不稳定度度量：窗口统计与四级判定
internal/quarantine   隔离策略：阈值 + 最小样本量，带审计日志
internal/baseline     命名基线对比：新出现不稳定 / 已恢复
internal/gate         运行放行判定：纯函数、可复现
internal/service      门面：串联以上各组件
```

## 判定语义

### 上报与归属

- 每次运行是一个 **批次（Batch）**，携带 `batch_id`、`source`、`run_index`
  （报告方提供的运行序号，如 CI build number）与若干用例结果。
- 批次**原子写入**：并发读永远看到完整批次，不会看到半写状态。
- 统计按 `run_index` 排序，与到达顺序无关——**乱序到达不改变最终统计**。
- 完全相同的重复上报是**幂等**的；同一 `(batch, test)` 上报不同状态会被
  拒绝（`conflicting_duplicate`），原始记录不变。
- 残缺/非法上报被拒绝并返回可区分的原因码：
  `missing_batch_id`、`missing_test_id`、`invalid_status`、`missing_source`、
  `negative_run_index`、`conflicting_duplicate`。

### 不稳定度判定（flakiness）

- 样本 = 窗口内的 pass/fail 次数；**skip 不计入样本**，也不影响翻转计数。
- `transitions` = 相邻运行间 pass↔fail 的翻转次数（skip 被跳过）。
- 判定优先级：
  1. `samples < min_samples` → **insufficient_data**（不给出确定性结论）；
  2. `failure_rate >= persistent_threshold` → **persistent_failure**（持续失败）；
  3. `failure_rate > flaky_threshold` 或 `transitions > 0` → **flaky**（偶发交替）；
  4. 否则 → **stable**。
- 分析窗口可配置为最近 N 次运行（`window_runs`，0 = 全部保留数据）。

### 隔离策略（quarantine）

- **只有 flaky（偶发不稳定）的用例才能进入隔离名单**；
  persistent_failure 明确排除并给出理由，样本不足者同样排除。
- 每次用新策略重新判定时，决策连同 `policy_version` 追加到审计日志
  （`GET /v1/quarantine/audit`），阈值调整后的历史重判定可解释、可追溯。

### 基线对比（baseline）

- 任意时刻可将当前画像快照为命名基线（如 `main`、`release-1`）。
- 对比输出每个用例的 `newly_flaky`（基线非 flaky、当前 flaky）与
  `recovered`（基线 flaky、当前已非 flaky）。
- 不同基线的快照与对比**相互独立、互不覆盖**，可同时查询。

### 运行放行（gate）

- 对给定的一批运行逐条检查失败用例：
  - 失败用例在隔离名单中 → 容忍（不阻断）；
  - 否则（含无历史记录的新用例）→ 阻断。
- 判定是 `(batches, reports, quarantine)` 的**纯函数**：相同输入与配置必然
  得到相同结论（迭代顺序均已排序确定化）。

### 数据保留（retention）

- `Prune` 按 `run_index` 保留最近 N 个批次，其余清理，存储占用有界。
- 统计始终从当前保留的批次**重新计算**，清理后剩余窗口内的统计不跳变、
  不残留旧聚合。

## 可配置项

| 配置 | 位置 | 默认 | 含义 |
|---|---|---|---|
| `window_runs` | flakiness.Config / `-window-runs` | 0（全部） | 分析窗口（最近 N 次运行） |
| `min_samples` | flakiness.Config / `-min-samples` | 3 | 给出确定性判定所需的最小 pass/fail 样本数 |
| `flaky_threshold` | flakiness.Config | 0.0 | 失败率超过该值判定为 flaky |
| `persistent_threshold` | flakiness.Config | 1.0 | 失败率达到该值判定为持续失败 |
| `policy.min_samples` | quarantine.Policy | 3 | 允许隔离的最小样本量 |
| `policy.max_failure_rate` | quarantine.Policy | 1.0 | 达到该失败率视为持续失败，禁止隔离 |
| `policy.version` | quarantine.Policy | `v1` | 策略版本，写入审计日志 |
| `retain_runs` | service.Config / `-retain-runs` | 100 | 保留的批次数（0 = 不限） |

## HTTP API

```
POST /v1/batches                    上报一个批次（幂等；冲突返回 409 + reason）
GET  /v1/flakiness                  当前窗口内全部用例的稳定性画像
POST /v1/quarantine/reevaluate      用新策略重新判定隔离名单（写入审计）
GET  /v1/quarantine                 当前隔离名单
GET  /v1/quarantine/audit           隔离决策历史
PUT  /v1/baselines/{name}           将当前画像快照为命名基线
GET  /v1/baselines/{name}/compare   与指定基线对比
POST /v1/gate                       放行判定，body: {"batch_ids":[...]}
POST /v1/retention/prune            执行保留回收
```

运行：

```sh
go run ./cmd/server -addr :8080 -retain-runs 100 -min-samples 3
```

## 测试

```sh
go test ./... -race
```

覆盖：重复与乱序上报、冲突拒绝、非法上报原因码、样本不足、阈值调整重判定、
并发读写（race detector）、隔离与放行判定、判定可复现性、基线对比与数据回收。
