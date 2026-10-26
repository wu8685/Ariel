import type {Item,Turn} from "./generated/protocol";
export type MessageEdit={turnId:string;itemId:string;text:string;fingerprint:string;agentEpoch:string;uncertain?:boolean};
export function editableMessage(turn:Turn):Item|null{
 const users=turn.items.filter(i=>i.role==="user");const item=users[0];
 return turn.status==="interrupted"&&users.length===1&&item.text.trim()&&!item.images?.length&&!/<(?:ariel_)?file_context|\[File context/.test(item.text)?item:null;
}
export function parseEditTarget(v:unknown):Omit<MessageEdit,"agentEpoch">|null{
 if(!v||typeof v!=="object")return null;const t=v as Record<string,unknown>;
 if(!["turnId","itemId","text","fingerprint"].every(k=>typeof t[k]==="string"&&!!t[k])||!(/^[a-f0-9]{64}$/).test(String(t.fingerprint))||String(t.text).length>65536)return null;
 return {turnId:String(t.turnId),itemId:String(t.itemId),text:String(t.text),fingerprint:String(t.fingerprint)};
}
// A missing latest anchor is not ordinary rotation. Discard derived history
// conservatively; bounded earlier pages can be explicitly loaded again.
export function historyRewritten(before:Turn[],after:Turn[]):boolean{
 const latest=before.at(-1);return !!latest&&!after.some(t=>t.turnId===latest.turnId);
}
