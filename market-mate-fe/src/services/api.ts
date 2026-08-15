export interface Ingredient {
  name: string;
  quantity: string;
}

export interface Store {
  name: string;
  address: string;
  distance: string;
  mapUrl: string;
}

export interface VideoMeta {
  id: string;
  title: string;
  channelTitle: string;
  thumbnailUrl: string;
}

export interface SearchLocation {
  latitude: number;
  longitude: number;
  label?: string;
  estimated: boolean;
}

export interface Provenance {
  video: boolean;
  ingredients: boolean;
  stores: boolean;
  any: boolean;
}

export interface RecipeResponse {
  ingredients: Ingredient[];
  stores: Store[];
  video?: VideoMeta;
  location: SearchLocation;
  simulated: Provenance;
  cached: boolean;
  notice?: string;
}

/** ApiError carries the server's message and the pipeline stage that failed,
 *  so the UI can say what actually went wrong instead of "something broke". */
export class ApiError extends Error {
  status: number;
  stage: string;

  constructor(status: number, message: string, stage = "") {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.stage = stage;
  }
}

// The base URL was a hardcoded http://localhost:8080 literal, which meant a
// built frontend could never talk to a deployed backend. It now comes from
// VITE_API_BASE_URL, defaulting to a same-origin relative path so a reverse
// proxy or single-host deploy works with no configuration at all.
const API_BASE = import.meta.env.VITE_API_BASE_URL ?? "";

export const processVideoUrl = async (videoUrl: string): Promise<RecipeResponse> => {
  let response: Response;
  try {
    response = await fetch(`${API_BASE}/api/process-video`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url: videoUrl }),
    });
  } catch {
    throw new ApiError(
      0,
      "Could not reach the MarketMate server. Is the backend running?",
      "network",
    );
  }

  if (!response.ok) {
    let message = `Request failed (${response.status})`;
    let stage = "";
    try {
      const body = await response.json();
      if (body?.error) message = body.error;
      if (body?.stage) stage = body.stage;
    } catch {
      /* non-JSON error body; keep the status-based message */
    }
    if (response.status === 429) {
      const retry = response.headers.get("Retry-After");
      message = retry
        ? `Too many requests. Try again in ${retry} seconds.`
        : "Too many requests. Try again shortly.";
    }
    throw new ApiError(response.status, message, stage);
  }

  return (await response.json()) as RecipeResponse;
};

export interface HealthResponse {
  status: string;
  mode: "live" | "demo";
  providers: { video: string; ingredients: string; stores: string };
}

export const getHealth = async (): Promise<HealthResponse | null> => {
  try {
    const res = await fetch(`${API_BASE}/api/health`);
    if (!res.ok) return null;
    return (await res.json()) as HealthResponse;
  } catch {
    return null;
  }
};
