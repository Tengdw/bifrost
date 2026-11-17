# 任务状态

本地任务用 `Status:` 字段记录状态，沿用技能的默认分流名称。

| 状态 | 含义 |
| --- | --- |
| `needs-triage` | 等待评估 |
| `needs-info` | 等待补充信息 |
| `ready-for-agent` | 规格完整，可由代理实施 |
| `ready-for-human` | 需要人工实施 |
| `wontfix` | 不予处理 |
| `in-progress` | 正在实施 |
| `done` | 已完成并通过验收 |

Wayfinder 决策任务的状态规则见 [任务跟踪](./issue-tracker.md)。
