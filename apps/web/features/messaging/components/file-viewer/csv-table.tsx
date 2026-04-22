"use client";

import { useMemo } from "react";

import { parseCsv } from "./utils";

export function CsvTable({ text }: { text: string }) {
  const rows = useMemo(() => parseCsv(text), [text]);
  if (rows.length === 0) {
    return <div className="p-4 text-[13px] text-muted-foreground">空文件</div>;
  }
  const [head, ...body] = rows;
  const headRow = head ?? [];
  return (
    <div className="p-4 overflow-auto">
      <table className="min-w-full text-[12px] border-collapse">
        <thead>
          <tr>
            {headRow.map((cell, i) => (
              <th
                key={i}
                className="border border-border bg-secondary px-2 py-1 text-left font-medium text-foreground"
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
    </div>
  );
}
