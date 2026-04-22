"use client";

export function ExcelSheet({
  sheets,
  activeIdx,
  onSelect,
}: {
  sheets: { name: string; rows: string[][] }[];
  activeIdx: number;
  onSelect: (i: number) => void;
}) {
  if (sheets.length === 0) {
    return <div className="p-4 text-[13px] text-muted-foreground">工作簿为空</div>;
  }
  const sheet = sheets[Math.min(activeIdx, sheets.length - 1)]!;
  const rows = sheet.rows;
  const [head, ...body] = rows;
  const headRow = head ?? [];
  return (
    <div className="h-full flex flex-col">
      {sheets.length > 1 && (
        <div className="flex items-center gap-1 px-2 py-1 border-b border-border bg-secondary/40 overflow-x-auto shrink-0">
          {sheets.map((s, i) => (
            <button
              key={s.name + i}
              type="button"
              onClick={() => onSelect(i)}
              className={`px-2 py-1 text-[12px] rounded-[4px] transition-colors whitespace-nowrap ${
                i === activeIdx
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:bg-accent/50 hover:text-foreground"
              }`}
            >
              {s.name}
            </button>
          ))}
        </div>
      )}
      <div className="flex-1 overflow-auto p-4">
        {rows.length === 0 ? (
          <div className="text-[13px] text-muted-foreground">该工作表为空</div>
        ) : (
          <table className="min-w-full text-[12px] border-collapse">
            <thead>
              <tr>
                {headRow.map((cell, i) => (
                  <th
                    key={i}
                    className="border border-border bg-secondary px-2 py-1 text-left font-medium text-foreground sticky top-0"
                  >
                    {cell}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {body.map((row, r) => (
                <tr key={r} className="hover:bg-accent/30">
                  {row.map((cell, c) => (
                    <td key={c} className="border border-border px-2 py-1 text-foreground align-top">
                      {cell}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
