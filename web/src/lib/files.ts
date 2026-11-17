// Client-side manuscript filtering, so we never upload node_modules or .git.
// Keep UPLOAD_EXT in sync with manuscriptExt in internal/server/server.go.

export const SKIP_DIRS = new Set([
  ".git", "node_modules", ".venv", "venv", "__pycache__", "target", ".next", ".github", ".idea", ".vscode", "vendor",
]);

const UPLOAD_EXT = new Set([
  "md", "mdx", "markdown", "ipynb", "txt", "yml", "yaml", "toml", "json",
  "png", "jpg", "jpeg", "pdf", "svg", "gif",
  "rs", "go", "py", "js", "ts", "c", "h", "java", "sh", "sql", "html", "css",
]);

const MAX_FILE = 50 * 1024 * 1024;

export interface PathedFile {
  file: File;
  path: string;
}

export function keepFile(path: string, size: number): boolean {
  const parts = path.split("/");
  if (parts.some((p) => SKIP_DIRS.has(p))) return false;
  const ext = (parts[parts.length - 1].split(".").pop() || "").toLowerCase();
  return UPLOAD_EXT.has(ext) && size <= MAX_FILE;
}

export const isEpub = (name: string) => /\.epub$/i.test(name);

export function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

/** Recursively read a dropped directory via the webkitGetAsEntry API. */
export async function readEntry(entry: FileSystemEntry, prefix: string, out: PathedFile[]): Promise<void> {
  if (entry.isFile) {
    const file = await new Promise<File>((res, rej) => (entry as FileSystemFileEntry).file(res, rej));
    out.push({ file, path: prefix + entry.name });
    return;
  }
  if (!entry.isDirectory || SKIP_DIRS.has(entry.name)) return;
  const reader = (entry as FileSystemDirectoryEntry).createReader();
  for (;;) {
    const batch = await new Promise<FileSystemEntry[]>((res, rej) => reader.readEntries(res, rej));
    if (!batch.length) break;
    for (const child of batch) await readEntry(child, `${prefix}${entry.name}/`, out);
  }
}

export function shortPath(p: string): string {
  const parts = p.split(/[\\/]/).filter(Boolean);
  return parts.length > 3 ? `…/${parts.slice(-3).join("/")}` : p;
}

const UPPER_ROMAN = ["", "I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X", "XI", "XII", "XIII", "XIV", "XV", "XVI", "XVII", "XVIII", "XIX", "XX"];
export const roman = (n: number) => UPPER_ROMAN[n] || String(n);
export const LOWER_ROMAN = ["i", "ii", "iii", "iv", "v", "vi", "vii", "viii", "ix", "x"];

export function hash(s: string): number {
  let h = 2166136261;
  for (const c of s) h = Math.imul(h ^ c.charCodeAt(0), 16777619);
  return h >>> 0;
}

export function initials(name: string): string {
  return name.split(/[\s,]+/).filter(Boolean).slice(0, 3).map((w) => w[0].toUpperCase()).join("");
}
