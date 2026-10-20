export class ApiError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

export async function getJSON<T>(
  path: string,
  signal?: AbortSignal,
): Promise<T> {
  const response = await fetch(path, {
    headers: { Accept: "application/json" },
    signal,
  });

  if (!response.ok) {
    const text = await response.text();
    let message = `Request failed with status ${response.status}`;
    if (text) {
      try {
        const body = JSON.parse(text) as { error?: string };
        message = body.error ?? text;
      } catch {
        message = text;
      }
    }

    throw new ApiError(message, response.status);
  }

  return response.json() as Promise<T>;
}
