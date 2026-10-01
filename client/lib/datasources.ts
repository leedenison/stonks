import { create } from "@bufbuild/protobuf";
import {
  type Datasource,
  DatasourceSchema,
  type ListDatasourcesResponse,
  ListDatasourcesResponseSchema,
} from "@/gen/admin/v1/admin_pb";

// reordered is the listing in the order of names, each precedence its
// position, which is what the service writes for that order. A name the
// listing lacks is left out.
export function reordered(
  list: ListDatasourcesResponse,
  names: string[],
): ListDatasourcesResponse {
  const byName = new Map<string, Datasource>(
    list.datasources.map((d) => [d.name, d]),
  );
  const datasources: Datasource[] = [];
  for (const name of names) {
    const d = byName.get(name);
    if (d) {
      datasources.push(
        create(DatasourceSchema, { ...d, precedence: datasources.length + 1 }),
      );
    }
  }
  return create(ListDatasourcesResponseSchema, { datasources });
}
