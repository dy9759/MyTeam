"use client";

import { useEffect } from "react";
import { useParams, useRouter } from "next/navigation";

export default function SessionDetailPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();

  useEffect(() => {
    if (id) {
      router.replace(`/session?id=${encodeURIComponent(id)}`);
    }
  }, [id, router]);

  return null;
}
