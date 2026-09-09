export function displaySafeUrl(
  value: string,
  invalidLabel = "Unparseable URL",
): string {
  try {
    const url = new URL(value);
    url.username = "";
    url.password = "";
    url.search = "";
    url.hash = "";
    return url.toString();
  } catch {
    return invalidLabel;
  }
}

export function displayMediaLocation(value: string): string {
  return displaySafeUrl(value, "Unparseable location");
}
