import { expect, it } from "vitest";
import { createQueryClient } from "../../shared/services";
import { card, deferred, page } from "../../shared/test-fixtures";
import type { Schema } from "../../shared/api";
import { confirmMatch } from "./cache";
export const match: Schema["Match"] = {
  subjectId: "subject",
  subjectRevision: 1,
  bindingRevision: 0,
  selection: null,
  cacheState: "absent",
  attemptState: "never",
  attemptedAt: null,
  errorCode: null,
  metadata: null,
};
it("confirmed matching cancels late old reads, updates matching subject only and never touches unrelated library/settings", async () => {
  const client = createQueryClient();
  const key = ["media", { id: card.id }];
  const old = { card, match };
  client.setQueryData(key, old);
  const delayed = deferred<typeof old>();
  let signal: AbortSignal | undefined;
  const read = client
    .fetchQuery({
      queryKey: key,
      staleTime: 0,
      queryFn: (ctx) => {
        signal = ctx.signal;
        return delayed.promise;
      },
    })
    .catch(() => {});
  const unrelated = ["catalog", { libraryId: "other" }];
  client.setQueryData(unrelated, { items: [card], page });
  client.setQueryData(["settings"], {});
  const confirmed = { ...match, bindingRevision: 2 };
  await confirmMatch(client, card.libraryId, confirmed);
  expect(signal?.aborted).toBe(true);
  delayed.resolve(old);
  await read;
  expect(
    client.getQueryData<{ match: Schema["Match"] }>(key)?.match.bindingRevision,
  ).toBe(2);
  expect(client.getQueryState(unrelated)?.isInvalidated).toBe(false);
  expect(client.getQueryState(["settings"])?.isInvalidated).toBe(false);
  client.clear();
});
