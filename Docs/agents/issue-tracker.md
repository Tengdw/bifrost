# 任务跟踪：本地 Markdown

任务保存在本仓库的 `.scratch/` 中，随 Git 版本管理。

- 每项功能一个目录：`.scratch/<feature-slug>/`。
- 全局规格以 `spec/SPEC.md` 为准，专项约定见 `Docs/README.md`。功能需要独立规格时写入 `.scratch/<feature-slug>/spec.md`，链接系统规格，避免复制全局规则。
- 每张实施任务单一个文件：`.scratch/<feature-slug>/issues/<NN>-<slug>.md`，从 `01` 起按依赖顺序编号。
- 任务包含 What to build、验收条件、`Blocked by` 和 `Status`；阻塞任务完成后才能开工。
- 待办状态使用 [任务状态](./triage-labels.md)；开始实施时设为 `in-progress`，完成验收后设为 `done`。`Blocked by` 中的所有任务均为 `done` 时解除阻塞。
- 评论和补充信息追加到任务文件的 `## Comments`。

技能要求发布任务时，创建对应文件；要求读取任务时，读取指定文件。拆票时先展示拆分和依赖，再按用户确认的内容发布。

## Wayfinder

- 地图：`.scratch/<effort>/map.md`，记录 Notes、Decisions-so-far 和 Fog。
- 决策任务：`.scratch/<effort>/issues/NN-<slug>.md`，`Type` 为 research、prototype、grilling 或 task。
- 决策任务单独使用 `claimed` / `resolved` 状态。依赖均为 `resolved` 的未认领任务按编号选择。
- 认领前保存 `Status: claimed`；解决后写入 `## Answer`，设为 `resolved`，在地图中追加结论与链接。
