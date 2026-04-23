import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";

import { AppShell } from "@/components/shared/app-shell";
import { Toaster } from "@/components/ui/sonner";
import { AppProviders } from "@/lib/providers";

import "./globals.css";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "chain-sync-watch",
  description: "Cross-source chain indexer verification dashboard",
};

// suppressHydrationWarning on <html> silences the next-themes class-
// mismatch warning — next-themes writes `class="dark"` on first
// client render before React rehydrates, and without the opt-out
// React compares server-rendered markup (no class) against the
// client DOM and logs noise.
export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="en"
      suppressHydrationWarning
      className={`${geistSans.variable} ${geistMono.variable} h-full antialiased`}
    >
      <body className="min-h-full">
        <AppProviders>
          <AppShell>{children}</AppShell>
          <Toaster richColors position="top-right" />
        </AppProviders>
      </body>
    </html>
  );
}
