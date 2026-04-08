"use client";

import { useParams, redirect } from "next/navigation";

export default function Page() {
  const { id } = useParams<{ id: string }>();
  redirect(`/session/${id}`);
}
