export async function getJSON<T>(path: string): Promise<T> {
  const response = await fetch(path, {
    headers: { Accept: "application/json" },
  });

  if (!response.ok) {
    const body = (await response.json().catch(() => undefined)) as
      | { error?: string }
      | undefined;

    throw new Error(
      body?.error ?? `Request failed with status ${response.status}`,
    );
  }

  return response.json() as Promise<T>;
}
