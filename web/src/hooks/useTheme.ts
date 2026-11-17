import { useCallback, useEffect, useState } from "react";

type Theme = "light" | "dark" | null; // null = follow the system

function readSaved(): Theme {
  try {
    const v = localStorage.getItem("folio-theme");
    return v === "light" || v === "dark" ? v : null;
  } catch {
    return null;
  }
}

export function useTheme() {
  const [theme, setTheme] = useState<Theme>(readSaved);

  useEffect(() => {
    const root = document.documentElement;
    if (theme) root.dataset.theme = theme;
    else delete root.dataset.theme;
  }, [theme]);

  const toggle = useCallback(() => {
    setTheme((cur) => {
      const effective = cur ?? (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
      const next = effective === "dark" ? "light" : "dark";
      try { localStorage.setItem("folio-theme", next); } catch { /* private mode */ }
      return next;
    });
  }, []);

  return toggle;
}
