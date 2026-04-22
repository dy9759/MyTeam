import type { Metadata, Viewport } from "next";
import { cookies } from "next/headers";
import { Inter, Source_Serif_4, JetBrains_Mono } from "next/font/google";
import { ThemeProvider } from "@/components/theme-provider";
import { Toaster } from "@/components/ui/sonner";
import { cn } from "@/lib/utils";
import { AuthInitializer } from "@/features/auth";
import { WSProvider } from "@/features/realtime";
import { ModalRegistry } from "@/features/modals";
import "./globals.css";

const inter = Inter({ subsets: ["latin"], variable: "--font-sans", weight: ["400", "500", "600", "700"] });
const sourceSerif = Source_Serif_4({
  subsets: ["latin"],
  variable: "--font-serif",
  weight: ["400", "500", "600", "700"],
  style: ["normal", "italic"],
});
const jetbrainsMono = JetBrains_Mono({ subsets: ["latin"], variable: "--font-mono" });

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#F7F3EC" },
    { media: "(prefers-color-scheme: dark)", color: "#1A140D" },
  ],
};

export const metadata: Metadata = {
  title: {
    default: "My Team — AI-Native Task Management",
    template: "%s | My Team",
  },
  description:
    "Open-source platform that turns coding agents into real teammates. Assign tasks, track progress, compound skills.",
  icons: {
    icon: [{ url: "/favicon.png", type: "image/png" }],
    shortcut: ["/favicon.png"],
  },
  openGraph: {
    type: "website",
    siteName: "My Team",
    locale: "en_US",
  },
  twitter: {
    card: "summary_large_image",
  },
  alternates: {
    canonical: "/",
  },
  robots: {
    index: true,
    follow: true,
  },
};

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const cookieStore = await cookies();
  const locale = cookieStore.get("myteam-locale")?.value;
  const lang = locale === "zh" ? "zh" : "en";

  return (
    <html
      lang={lang}
      suppressHydrationWarning
      className={cn(
        "antialiased font-sans h-full",
        inter.variable,
        sourceSerif.variable,
        jetbrainsMono.variable,
      )}
    >
      <body className="h-full overflow-hidden" suppressHydrationWarning>
        <ThemeProvider>
          <AuthInitializer>
            <WSProvider>{children}</WSProvider>
          </AuthInitializer>
          <ModalRegistry />
          <Toaster />
        </ThemeProvider>
      </body>
    </html>
  );
}
