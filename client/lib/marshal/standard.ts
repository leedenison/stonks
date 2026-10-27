// The standard format: one JSON document holding a Stmt in its protojson
// encoding. The proto file is the definition of the format, so a field of the
// message is a field of the document.

import { fromJsonString } from "@bufbuild/protobuf";
import { Broker } from "@/gen/type/v1/type_pb";
import { type Stmt, StmtSchema } from "@/gen/stmt/v1/stmt_pb";
import { MarshalError } from "./error";
import type { Marshaller } from "./marshal";
import { mediaType } from "./media";

const TYPE = "application/json";

// parse requires the broker and the period, which the dialog shows before
// submission.
function parse(text: string): Stmt {
  let s: Stmt;
  try {
    s = fromJsonString(StmtSchema, text);
  } catch (e) {
    throw new MarshalError(e instanceof Error ? e.message : String(e));
  }
  if (s.broker === Broker.UNSPECIFIED) {
    throw new MarshalError("the statement states no broker");
  }
  if (s.orderFrom === "" || s.orderBefore === "") {
    throw new MarshalError("the statement states no period");
  }
  return s;
}

// marshal only parses the document. The server validates the message as it
// does every statement.
function marshal(text: string): Stmt {
  return parse(text);
}

function recognise(text: string, type: string): boolean {
  if (mediaType(type) !== TYPE) return false;
  try {
    parse(text);
    return true;
  } catch {
    return false;
  }
}

export const standard: Marshaller = {
  marshal,
  recognise,
  // Every quantity is as at its row's as_at.
  needsExportDate: false,
};
