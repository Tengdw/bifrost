import { useEffect, useState } from "react";
import { Alert, Table } from "antd";
import { positions } from "../../api/snapshots";
export function Positions({
  accountIds,
  batchId,
  complete,
}: {
  accountIds: string[];
  batchId: string;
  complete: boolean;
}) {
  const [rows, setRows] = useState<Awaited<ReturnType<typeof positions>>>([]);
  const [failed, setFailed] = useState(false);
  const [loading, setLoading] = useState(true);
  useEffect(() => {
    const c = new AbortController();
    setFailed(false);
    setLoading(true);
    setRows([]);
    positions(c.signal, accountIds, batchId)
      .then((data) => {
        if (!c.signal.aborted) {
          setRows(data);
          setLoading(false);
        }
      })
      .catch(() => {
        if (!c.signal.aborted) {
          setFailed(true);
          setLoading(false);
        }
      });
    return () => c.abort();
  }, [accountIds.join(","), batchId]);
  return (
    <section>
      <h2>合约仓位</h2>
      <p>仓位名义价值单列，不计入组合权益。</p>
      {failed ? (
        <Alert type="error" message="仓位读取失败" />
      ) : (
        <Table
          rowKey={(r) => r.accountId + "/" + r.symbol + "/" + r.side}
          dataSource={rows}
          loading={loading}
          scroll={{ x: 650 }}
          pagination={false}
          locale={{
            emptyText: complete
              ? "没有合约仓位"
              : "本批未确认合约仓位，请核对钱包状态",
          }}
          columns={[
            { title: "账号", dataIndex: "accountId" },
            { title: "合约", dataIndex: "symbol" },
            { title: "方向", dataIndex: "side" },
            { title: "账户模式", dataIndex: "mode" },
            { title: "数量", dataIndex: "quantity" },
            { title: "保证金币", dataIndex: "marginAsset" },
            { title: "未实现盈亏", dataIndex: "unrealized" },
            { title: "名义价值", dataIndex: "notional" },
          ]}
        />
      )}
    </section>
  );
}
