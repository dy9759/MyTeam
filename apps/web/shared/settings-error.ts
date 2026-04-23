function hasCjkCharacters(text: string): boolean {
  return /[\u3400-\u9fff]/.test(text);
}

export function getSettingsErrorMessage(error: unknown, fallback: string): string {
  const message =
    typeof error === "string"
      ? error.trim()
      : error instanceof Error
        ? error.message.trim()
        : "";

  if (message && hasCjkCharacters(message)) {
    return message;
  }

  return fallback;
}
