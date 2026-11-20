// @kiflet/api: the types of Kiflet's API (/api/v1), generated from
// api/openapi/v1.yaml in Kiflet/backend, and a client typed by them.
import type { Client, ClientOptions } from "openapi-fetch";
import type { components, paths } from "./schema.d.ts";

export type { components, operations, paths, webhooks } from "./schema.d.ts";

/** A schema of the API by its name: `Schema<"Shipment">`. */
export type Schema<Name extends keyof components["schemas"]> = components["schemas"][Name];

/**
 * A client of the API whose paths, parameters, bodies and answers are typed
 * by the specification. `baseUrl` is the API's, ending in /api/v1.
 */
export declare function createKifletClient(options?: ClientOptions): Client<paths>;
