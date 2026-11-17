import { useEffect, useState } from "react";
import { Alert, Button, Select, Table } from "antd";
import { accounts, refresh, sync, RequestError } from "../../api/snapshots";
import type { Account, SyncStatus, Wallet } from "../../api/snapshots";
import { Latest } from "./Latest";
const walletOptions = [
  { value: "spot", label: "现货" },
  { value: "funding", label: "资金" },
  { value: "earn", label: "赚币" },
  { value: "usdm", label: "U 本位合约" },
];
export function Workspace() {
  const [items, setItems] = useState<Account[]>([]);
  const [selected, setSelected] = useState<string[]>([]);
  const [wallets, setWallets] = useState<Wallet[]>([]);
  const [version, setVersion] = useState(0);
  const [error, setError] = useState("");
  const [statuses, setStatuses] = useState<SyncStatus[]>([]);
  const [tasks, setTasks] = useState<string[]>([]);
  const [sending, setSending] = useState(false);
  useEffect(() => {
    const c = new AbortController();
    accounts(c.signal)
      .then(setItems)
      .catch((e: unknown) => {
        if (!c.signal.aborted)
          setError(
            e instanceof RequestError && e.status === 401
              ? "会话已过期，请重新登录。"
              : "账号列表读取失败，请重试。",
          );
      });
    return () => c.abort();
  }, [version]);
  useEffect(() => {
    if (!tasks.length) return;
    const c = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        const rows = (
          await Promise.all(tasks.map((id) => sync(id, c.signal)))
        ).flat();
        if (c.signal.aborted) return;
        setStatuses(rows);
        if (rows.some((r) => r.state === "running"))
          timer = setTimeout(() => void poll(), 1000);
        else {
          setTasks([]);
          setVersion((v) => v + 1);
        }
      } catch {
        if (!c.signal.aborted) {
          setError("刷新状态读取失败，请重新读取。");
          setTasks([]);
        }
      }
    }
    void poll();
    return () => {
      c.abort();
      clearTimeout(timer);
    };
  }, [tasks]);
  async function request() {
    setError("");
    setSending(true);
    try {
      const ids = selected.length
        ? selected
        : items.filter((a) => !a.disabledAt).map((a) => a.id);
      if (!ids.length) {
        setError("请先绑定或选择已启用账号。");
        return;
      }
      const result = await refresh(ids, wallets.length ? wallets : undefined);
      setTasks([...new Set(result.map((r) => r.batchId))]);
      if (result.some((r) => r.state === "already_running"))
        setError("正在复用已有刷新任务。");
    } catch {
      setError("无法开始刷新，请确认账号已启用并重试。");
    } finally {
      setSending(false);
    }
  }
  return (
    <>
      <section>
        <h2>查看与刷新</h2>
        <div className="controls">
          <label>
            账号范围
            <Select
              mode="multiple"
              aria-label="账号范围"
              placeholder="全部启用账号"
              value={selected}
              onChange={setSelected}
              options={items.map((a) => ({
                value: a.id,
                label: a.alias + (a.disabledAt ? "（已停用）" : ""),
              }))}
            />
          </label>
          <label>
            刷新钱包
            <Select
              mode="multiple"
              aria-label="刷新钱包"
              placeholder="四个钱包"
              value={wallets}
              onChange={setWallets}
              options={walletOptions}
            />
          </label>
          <Button loading={sending} onClick={() => void request()}>
            立即采集
          </Button>
          <Button onClick={() => setVersion((v) => v + 1)}>重新读取</Button>
        </div>
        {error && <Alert type="info" message={error} />}
        {statuses.length > 0 && (
          <Table
            scroll={{ x: 650 }}
            rowKey={(r) => r.batchId + r.accountId + r.wallet}
            pagination={false}
            dataSource={statuses}
            columns={[
              {
                title: "账号",
                dataIndex: "accountId",
                render: (id: string) =>
                  items.find((a) => a.id === id)?.alias || id,
              },
              { title: "钱包", dataIndex: "wallet" },
              { title: "状态", dataIndex: "state" },
              { title: "说明", dataIndex: "reason" },
            ]}
          />
        )}
      </section>
      <Latest accountIds={selected} revision={version} managed />
    </>
  );
}
