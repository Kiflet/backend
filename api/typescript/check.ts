// Not published: uses the package as Kiflet/web and Kiflet/mobile will, so
// that npm run check fails if its types stop working together.
import { createKifletClient, type Schema } from "./index.js";

const api = createKifletClient({ baseUrl: "https://app.kiflet.et/api/v1" });

export async function shipment(ref: string): Promise<Schema<"Shipment"> | undefined> {
  const { data } = await api.GET("/shipments/{ref}", { params: { path: { ref } } });
  return data;
}

export async function sync(body: Schema<"SyncRequest">): Promise<Schema<"SyncResponse"> | undefined> {
  const { data } = await api.POST("/driver/sync", { body });
  return data;
}
