"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ReactQueryDevtools } from "@tanstack/react-query-devtools";
import { ThemeProvider } from "next-themes";
import { useState } from "react";

// AppProviders wires every client-side context the tree needs:
// TanStack Query (server state cache) and next-themes (dark mode).
// The QueryClient lives in useState so the reference is stable
// across the tree's lifetime; React 19 strict-mode double-invocation
// still yields a single instance.
//
// refetchOnWindowFocus is disabled by default to keep the dashboard
// calm under operator multitasking — long-running verification jobs
// don't change on tab focus, they change when the worker finishes.
export function AppProviders({ children }: { children: React.ReactNode }) {
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            staleTime: 30_000,
            refetchOnWindowFocus: false,
            retry: 1,
          },
        },
      }),
  );

  return (
    <ThemeProvider attribute="class" defaultTheme="system" enableSystem disableTransitionOnChange>
      <QueryClientProvider client={queryClient}>
        {children}
        {process.env.NODE_ENV === "development" ? (
          <ReactQueryDevtools initialIsOpen={false} buttonPosition="bottom-right" />
        ) : null}
      </QueryClientProvider>
    </ThemeProvider>
  );
}
