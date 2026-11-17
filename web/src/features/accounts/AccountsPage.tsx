import { useEffect, useState } from "react";
import { Alert, Button } from "antd";
import { accounts, RequestError } from "../../api/snapshots";
import type { Account } from "../../api/snapshots";
import { Replay } from "../replay/Replay";
import { Accounts } from "./Accounts";

export function AccountsPage({
  onUnauthorized,
  replayAvailable,
}: {
  onUnauthorized: () => void;
  replayAvailable: boolean;
}) {
  const [items, setItems] = useState<Account[]>([]);
  const [selected, setSelected] = useState<string>();
  const [error, setError] = useState(false);
  const [revision, setRevision] = useState(0);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    accounts(controller.signal)
      .then((next) => {
        if (!controller.signal.aborted) {
          setItems(next);
          setLoaded(true);
          setError(false);
        }
      })
      .catch((failure: unknown) => {
        if (controller.signal.aborted) return;
        if (failure instanceof RequestError && failure.status === 401)
          onUnauthorized();
        else setError(true);
      });
    return () => controller.abort();
  }, [revision, onUnauthorized]);

  const update = () => setRevision((n) => n + 1);
  const target = items.find((account) => account.id === selected);
  return (
    <>
      {error && (
        <Alert
          type="error"
          showIcon
          message="账号列表读取失败"
          action={
            <Button size="small" onClick={update}>
              重新读取
            </Button>
          }
        />
      )}
      {loaded ? (
        <Accounts
          items={items}
          onChange={update}
          onReplay={
            replayAvailable ? (account) => setSelected(account.id) : undefined
          }
        />
      ) : (
        !error && <p role="status">正在读取账号…</p>
      )}
      {target && (
        <Replay
          key={target.id}
          account={target}
          onUnauthorized={onUnauthorized}
        />
      )}
    </>
  );
}
