# flakiness-tracker-go

测试不稳定度跟踪与可靠性判定服务：把多次运行的用例结果长期汇总成可查询的稳定性画像，并据此完成用例隔离与运行放行的工程决策。

## 核心概念

| 概念 | 类型 | 说明 |
| --- | --- | --- |
| 运行批次 | `Batch` | 一次运行的上报单元，含 `ID`、`Source`、`Seq`（运行序号）、`StartedAt` 与用例结果列表 |
| 用例结果 | `TestResult` | `TestID` + `Outcome`（`passed` / `failed` / `skipped`） |
| 统计窗口 | `Window` | `LastRuns > 0` 时取最近 N 个批次（按 `Seq` 排序的尾部），否则取全部 |
| 统计画像 | `TestStats` | 窗口内通过/失败/跳过计数、失败率与稳定性分类 |
| 隔离策略 | `QuarantinePolicy` | 阈值 + 最小样本量，决定哪些用例进入隔离名单 |
| 放行判定 | `GateConfig` / `GateReport` | 对一组运行给出 allow/block 结论及理由 |

## 接入语义（`Store.Ingest`）

- **幂等**：同一 `(batchID, testID)` 重复上报相同结果不改变统计；重复批次整体重放安全。
- **乱序容忍**：统计窗口按 `Seq` 排序，与批次到达顺序无关。
- **拒绝可区分**：非法上报返回 `*RejectError`，其 `Reason` 字段可程序化区分：
  - `empty_batch_id`、`empty_test_id`、`empty_results`
  - `non_positive_seq`
  - `unknown_outcome`
  - `conflicting_result`（同一用例在同一批次内或跨重放出现不同结果）
- 被拒绝的批次不会部分写入。

## 稳定性分类（`Store.Stats` / `StatsFor`）

窗口内 `counted = passed + failed`（`skipped` 不计入不稳定度）：

| 分类 | 条件 |
| --- | --- |
| `insufficient_data` | `counted < MinSamples`，**不给出确定性结论** |
| `stable` | `failed == 0` |
| `consistently_failing` | `failed == counted`（持续失败，**不是**不稳定） |
| `flaky` | 其余（通过/失败交替出现） |

## 隔离策略（`Store.Quarantine`）

只有分类为 `flaky` 且 `FlakeRateMin <= FailureRate < FlakeRateMax` 的用例进入隔离名单；持续失败用例失败率为 1.0，天然被 `FlakeRateMax` 排除，不会被误判为不稳定。

每次调用都会追加一条 `Evaluation` 审计记录（策略版本、完整策略快照、判定时间、每条名单记录的判定理由），通过 `Store.Evaluations()` 查询，阈值或策略调整后的重新判定可解释、可追溯。

## 基线对比（`Store.CompareWithBaseline`）

将当前窗口统计与基线快照（`testID -> Classification`，通常来自基线分支/基线运行）对比：

- `newly_flaky`：基线中稳定或不存在，当前为 flaky；
- `recovered`：基线中 flaky，当前明确恢复为 stable（样本不足不算恢复）；
- `unchanged`：其余。

结论按 `(baselineID, testID)` 独立存储，同一用例关联多个基线时可同时查询、互不覆盖（`Store.BaselineVerdictFor`）。

## 放行判定（`Store.Decide`）

对给定批次集合，取其中失败的用例，按窗口统计与隔离名单分类：

1. 批次不存在 → **block**；
2. 失败用例命中隔离名单 → 记录，不阻断；
3. 失败用例为持续失败 → **block**；
4. 失败用例为未隔离 flaky → `BlockOnFlaky` 为真时 **block**，否则仅记录；
5. 其余 → **allow**。

判定是存储快照与配置的纯函数：相同输入与相同配置必得相同结论（可复现）。

## 数据保留与回收

- `PruneBeforeSeq(seq)` / `PruneBeforeTime(t)` 清理窗口外的旧批次；
- 统计只基于剩余批次计算，清理窗口外数据不会使窗口内统计跳变；
- 配合固定 `Window.LastRuns` 与定期清理，长期运行存储占用有界。

## 并发模型

`Store` 内部使用 `sync.RWMutex`：写入互斥，读取在锁内完成并返回拷贝快照，任一时刻读到的都是自洽的批次与用例视图，不存在半写状态。`go test -race ./...` 通过。

## 可配置项汇总

| 配置 | 位置 | 含义 |
| --- | --- | --- |
| `Window.LastRuns` | 统计/策略/判定 | 窗口内最近运行批次数，`<=0` 表示全部 |
| `MinSamples` | 统计/策略/判定 | 最小有效样本量，不足则为 `insufficient_data` |
| `QuarantinePolicy.FlakeRateMin` | 隔离策略 | 隔离失败率下界（含） |
| `QuarantinePolicy.FlakeRateMax` | 隔离策略 | 隔离失败率上界（不含），排除持续失败 |
| `QuarantinePolicy.Version` | 隔离策略 | 策略版本号，写入审计记录 |
| `GateConfig.BlockOnFlaky` | 放行判定 | 未隔离 flaky 用例失败是否阻断 |

## 测试

```sh
go test -race ./...
```

覆盖：重复与乱序上报、非法上报拒绝原因、样本不足、阈值调整与审计、并发读写、隔离与放行判定、基线对比多基线共存、数据回收后窗口统计稳定。
