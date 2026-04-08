export default function AgentDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  return (
    <div className="p-6">
      <h1 className="text-2xl font-bold">代理详情</h1>
      <p className="mt-2 text-muted-foreground">代理状态和任务历史</p>
    </div>
  );
}
