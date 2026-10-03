import Ajv2020 from "ajv/dist/2020.js";
import schema from "../../protocol/v1.schema.json";
import type { ArielProtocolV1Envelope } from "./generated/protocol";

// Conditional `required` clauses refer to fields declared at the parent level.
const ajv = new Ajv2020({ strict: true, strictRequired: false, allErrors: false });
const validate = ajv.compile(schema);

export function validateEnvelope(value: unknown): value is ArielProtocolV1Envelope {
  return validate(value) === true;
}
