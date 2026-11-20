# @kiflet/api

The types of Kiflet's API (`/api/v1`) and a client typed by them, generated
from [`api/openapi/v1.yaml`](../openapi/v1.yaml) with openapi-typescript
and openapi-fetch. Kiflet/web and Kiflet/mobile depend on a version of it,
so a change to the API shows up there as a type error.

```ts
import { createKifletClient, type Schema } from "@kiflet/api";

const api = createKifletClient({ baseUrl: "https://app.kiflet.et/api/v1" });
const { data, error } = await api.GET("/shipments/{ref}", { params: { path: { ref } } });
const shipment: Schema<"Shipment"> | undefined = data;
```

It is published to GitHub Packages (`https://npm.pkg.github.com`) by
`.github/workflows/api-package.yml` whenever the specification changes on
`main`, as version `1.0.<run>`. Don't edit `schema.d.ts`: `make api`
writes it, and CI fails if it differs from the specification.
