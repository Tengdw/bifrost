import { useState } from "react";
import { Alert, Button, Modal, Table } from "antd";
import {
  bindAccount,
  rotateAccount,
  disableAccount,
  renameAccount,
  RequestError,
} from "../../api/snapshots";
import type { Account } from "../../api/snapshots";
function failure(error: unknown) {
  return error instanceof RequestError && error.status === 409
    ? "账号身份冲突：请核对新凭据是否属于同一交易所账号。"
    : "操作失败，请检查只读权限、连接和输入后重试。";
}
export function Accounts({
  items,
  onChange,
}: {
  items: Account[];
  onChange: () => void;
}) {
  const [target, setTarget] = useState<Account | null | undefined>(undefined);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    const credentials = {
      apiKey: String(data.get("apiKey")),
      apiSecret: String(data.get("apiSecret")),
    };
    setBusy(true);
    setError("");
    try {
      if (target) await rotateAccount(target.id, credentials);
      else
        await bindAccount({ ...credentials, alias: String(data.get("alias")) });
      form.reset();
      setTarget(undefined);
      onChange();
    } catch (e: unknown) {
      setError(failure(e));
      form.reset();
    } finally {
      setBusy(false);
    }
  }
  async function disable(account: Account) {
    setBusy(true);
    try {
      await disableAccount(account.id);
      onChange();
    } catch (e: unknown) {
      setError(failure(e));
    } finally {
      setBusy(false);
    }
  }
  async function rename(account: Account, alias: string) {
    if (!alias.trim()) return;
    try {
      await renameAccount(account.id, alias.trim());
      onChange();
    } catch (e: unknown) {
      setError(failure(e));
    }
  }
  return (
    <section>
      <h2>账号管理</h2>
      {error && <Alert type="error" message={error} />}
      <Button
        onClick={() => {
          setError("");
          setTarget(null);
        }}
      >
        绑定只读账号
      </Button>
      <Table
        scroll={{ x: 650 }}
        rowKey="id"
        dataSource={items}
        pagination={false}
        columns={[
          {
            title: "账号别名",
            render: (_: unknown, a: Account) => (
              <input
                aria-label={"别名 " + a.alias}
                defaultValue={a.alias}
                key={a.alias}
                onBlur={(e) => {
                  if (e.target.value !== a.alias)
                    void rename(a, e.target.value);
                }}
              />
            ),
          },
          {
            title: "状态",
            render: (_: unknown, a: Account) =>
              a.disabledAt ? "已停用，历史保留" : "已启用",
          },
          {
            title: "操作",
            render: (_: unknown, a: Account) => (
              <>
                <Button
                  disabled={!!a.disabledAt}
                  onClick={() => {
                    setError("");
                    setTarget(a);
                  }}
                >
                  更换密钥
                </Button>{" "}
                <Button
                  disabled={busy || !!a.disabledAt}
                  onClick={() =>
                    Modal.confirm({
                      title: "停用 " + a.alias + "？",
                      content: "停止后续刷新，历史仍可查。",
                      okText: "停用",
                      cancelText: "取消",
                      onOk: () => disable(a),
                    })
                  }
                >
                  停用
                </Button>
              </>
            ),
          },
        ]}
      />
      <Modal
        open={target !== undefined}
        title={target ? "轮换 " + target.alias + " 的只读凭据" : "绑定只读账号"}
        footer={null}
        onCancel={() => {
          if (!busy) setTarget(undefined);
        }}
        destroyOnHidden
      >
        <form onSubmit={submit} autoComplete="off" className="account-form">
          {!target && (
            <label>
              账号别名
              <input name="alias" required maxLength={80} />
            </label>
          )}
          <label>
            API Key
            <input name="apiKey" type="password" required autoComplete="off" />
          </label>
          <label>
            API Secret
            <input
              name="apiSecret"
              type="password"
              required
              autoComplete="new-password"
            />
          </label>
          <p>只读凭据；保存后不回显。</p>
          <Button htmlType="submit" loading={busy}>
            验证并保存
          </Button>
        </form>
      </Modal>
    </section>
  );
}
