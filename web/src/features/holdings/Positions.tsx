import { useEffect, useState } from "react";
import { Alert, Table } from "antd";
import { positions } from "../../api/snapshots";
export function Positions({
  accountIds,
  batchId,
}: {
  accountIds: string[];
  batchId: string;
}) {
  const [rows, setRows] = useState<Awaited<ReturnType<typeof positions>>>([]);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    const c = new AbortController();
    setFailed(false);
    setRows([]);
    positions(c.signal, accountIds, batchId)
      .then(setRows)
      .catch(() => {
        if (!c.signal.aborted) setFailed(true);
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
          scroll={{ x: 650 }}
          pagination={false}
          locale={{ emptyText: "没有合约仓位" }}
          columns={[
            { title: "账号", dataIndex: "accountId" },
            { title: "合约", dataIndex: "symbol" },
            { title: "方向", dataIndex: "side" },
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
