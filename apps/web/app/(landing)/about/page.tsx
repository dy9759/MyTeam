import type { Metadata } from "next";
import { AboutPageClient } from "@/features/landing/components/about-page-client";

export const metadata: Metadata = {
  title: "About",
  description:
    "Learn about My Team — multiplexed information and computing agent. An open-source AI-native task management platform.",
  openGraph: {
    title: "About My Team",
    description:
      "The story behind My Team and why we're building AI-native task management.",
    url: "/about",
  },
  alternates: {
    canonical: "/about",
  },
};

export default function AboutPage() {
  return <AboutPageClient />;
}
