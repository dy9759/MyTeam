export type Kind =
  | "markdown"
  | "html"
  | "text"
  | "code"
  | "csv"
  | "image"
  | "pdf"
  | "excel"
  | "office"
  | "unknown";

export const CODE_EXT = new Set([
  "ts", "tsx", "js", "jsx", "py", "go", "rs", "java", "c", "cpp", "h",
  "cs", "rb", "php", "swift", "kt", "scala", "sh", "bash", "zsh",
  "sql", "toml", "xml", "html", "css", "scss", "vue", "svelte",
]);

export const TEXT_EXT = new Set(["txt", "log", "env", "gitignore"]);
export const DATA_EXT = new Set(["json", "yaml", "yml"]);

export function detectKind(name: string, mime?: string): Kind {
  const ext = name.split(".").pop()?.toLowerCase() ?? "";
  if (ext === "md" || ext === "markdown" || mime === "text/markdown") return "markdown";
  if (ext === "html" || ext === "htm" || mime === "text/html") return "html";
  if (ext === "csv" || mime === "text/csv") return "csv";
  if (ext === "pdf" || mime === "application/pdf") return "pdf";
  if (["png", "jpg", "jpeg", "gif", "webp", "svg", "bmp"].includes(ext)) return "image";
  if (mime?.startsWith("image/")) return "image";
  if (ext === "xlsx" || ext === "xls" || mime?.includes("spreadsheetml") || mime === "application/vnd.ms-excel") return "excel";
  if (["doc", "docx", "ppt", "pptx"].includes(ext)) return "office";
  if (CODE_EXT.has(ext)) return "code";
  if (DATA_EXT.has(ext)) return "code";
  if (TEXT_EXT.has(ext) || mime?.startsWith("text/")) return "text";
  return "unknown";
}

export function typeBadge(kind: Kind, ext: string) {
  if (kind === "markdown") return "MD";
  if (kind === "html") return "HTML";
  if (kind === "pdf") return "PDF";
  if (kind === "image") return "IMG";
  if (kind === "excel") return "XLSX";
  if (kind === "office") return ext.toUpperCase();
  if (kind === "csv") return "CSV";
  return ext.toUpperCase() || "FILE";
}

// Small CSV parser — handles quoted fields with embedded commas/newlines and
// escaped quotes ("" → "). Good enough for preview; not a full RFC 4180
// implementation.
export function parseCsv(text: string): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = "";
  let inQuotes = false;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (inQuotes) {
      if (c === '"') {
        if (text[i + 1] === '"') {
          field += '"';
          i++;
        } else {
          inQuotes = false;
        }
      } else {
        field += c;
      }
      continue;
    }
    if (c === '"') {
      inQuotes = true;
    } else if (c === ",") {
      row.push(field);
      field = "";
    } else if (c === "\n" || c === "\r") {
      if (c === "\r" && text[i + 1] === "\n") i++;
      row.push(field);
      field = "";
      rows.push(row);
      row = [];
    } else {
      field += c;
    }
  }
  if (field.length > 0 || row.length > 0) {
    row.push(field);
    rows.push(row);
  }
  return rows.filter((r) => r.some((c) => c.length > 0));
}
