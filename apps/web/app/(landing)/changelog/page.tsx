import type { Metadata } from "next";
import { ChangelogPageClient } from "@/features/landing/components/changelog-page-client";

export const metadata: Metadata = {
  title: "Changelog",
  description:
    "See what's new in My Team — latest features, improvements, and fixes.",
  openGraph: {
    title: "Changelog | My Team",
    description: "Latest updates and releases from My Team.",
    url: "/changelog",
  },
  alternates: {
    canonical: "/changelog",
  },
};

export default function ChangelogPage() {
  return <ChangelogPageClient />;
}
