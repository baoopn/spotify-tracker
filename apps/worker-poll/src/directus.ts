import { DIRECTUS_TOKEN, DIRECTUS_URL } from "./config.ts";

interface DirectusItemsResponse<T> {
  data: T[];
}

interface DirectusItemResponse<T> {
  data: T;
}

export interface DirectusListOptions {
  filter?: Record<string, unknown>;
  limit?: number;
  fields?: string[];
  sort?: string[];
}

const directusBaseUrl = new URL(DIRECTUS_URL);
const basePath = directusBaseUrl.pathname.replace(/\/+$/, "");
const itemsBasePath = basePath.endsWith("/items")
  ? basePath
  : `${basePath}/items`;

export async function directusFetch<T>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const queryStart = path.indexOf("?");
  const rawPath = queryStart >= 0 ? path.slice(0, queryStart) : path;
  const rawQuery = queryStart >= 0 ? path.slice(queryStart + 1) : "";
  const normalizedPath = rawPath.startsWith("/") ? rawPath : `/${rawPath}`;
  const url = new URL(directusBaseUrl);
  url.pathname = `${itemsBasePath}${normalizedPath}`;
  url.search = rawQuery ? `?${rawQuery}` : "";

  const response = await fetch(url, {
    ...init,
    headers: {
      Authorization: `Bearer ${DIRECTUS_TOKEN}`,
      "Content-Type": "application/json",
      ...init.headers,
    },
  });

  if (!response.ok) {
    const body = await response.text();
    throw new Error(`Directus request failed (${response.status}): ${body}`);
  }

  return (await response.json()) as T;
}

function buildListQuery(options: DirectusListOptions = {}): string {
  const params = new URLSearchParams();

  if (options.limit !== undefined) {
    params.set("limit", String(options.limit));
  }

  if (options.fields && options.fields.length > 0) {
    params.set("fields", options.fields.join(","));
  }

  if (options.sort && options.sort.length > 0) {
    params.set("sort", options.sort.join(","));
  }

  if (options.filter) {
    for (const [field, condition] of Object.entries(options.filter)) {
      if (typeof condition === "object" && condition !== null) {
        for (const [op, value] of Object.entries(condition)) {
          params.set(`filter[${field}][${op}]`, String(value));
        }
      }
    }
  }

  const query = params.toString();
  return query ? `?${query}` : "";
}

export async function readItems<T>(
  collection: string,
  options: DirectusListOptions = {},
): Promise<T[]> {
  const query = buildListQuery(options);
  const result = await directusFetch<DirectusItemsResponse<T>>(
    `/${collection}${query}`,
  );
  return result.data;
}

export async function createItem<T>(
  collection: string,
  payload: T,
): Promise<T> {
  const result = await directusFetch<DirectusItemResponse<T>>(
    `/${collection}`,
    {
      method: "POST",
      body: JSON.stringify(payload),
    },
  );
  return result.data;
}

export async function updateItem<T>(
  collection: string,
  id: string,
  payload: Partial<T>,
): Promise<T> {
  const result = await directusFetch<DirectusItemResponse<T>>(
    `/${collection}/${id}`,
    {
      method: "PATCH",
      body: JSON.stringify(payload),
    },
  );
  return result.data;
}

export async function updateSingletonItem<T>(
  collection: string,
  payload: Partial<T>,
): Promise<T> {
  const result = await directusFetch<DirectusItemResponse<T>>(
    `/${collection}`,
    {
      method: "PATCH",
      body: JSON.stringify(payload),
    },
  );
  return result.data;
}
