import {describe,it,expect} from "vitest";
import {editableMessage,parseEditTarget,historyRewritten} from "./message-edit";
import type {Turn} from "./generated/protocol";
const turn=(id:string,status:Turn["status"]="completed"):Turn=>({turnId:id,status,items:[{itemId:"user",role:"user",text:"original"}]});
describe("stopped message editing",()=>{
 it("only offers the interrupted plain text latest user message",()=>{
  expect(editableMessage(turn("t","interrupted"))?.itemId).toBe("user");
  expect(editableMessage(turn("t"))).toBeNull();
  const attached=turn("t","interrupted");attached.items[0].text="<ariel_file_context>private</ariel_file_context>";
  expect(editableMessage(attached)).toBeNull();
  attached.items[0].text="text";attached.items[0].images=[{kind:"native",index:0,alt:"image",source:""}];expect(editableMessage(attached)).toBeNull();
 });
 it("parses only a fully identified original and rejects partial receipts",()=>{
  const target={turnId:"t",itemId:"user",text:"original",fingerprint:"a".repeat(64)};
  expect(parseEditTarget(target)).toEqual(target);expect(parseEditTarget({...target,fingerprint:"stale"})).toBeNull();
 });
 it("invalidates pagination on replacement but not normal two-turn rotation",()=>{
  expect(historyRewritten([turn("seed"),turn("old","interrupted")],[turn("seed"),turn("new")])).toBe(true);
  expect(historyRewritten([turn("a"),turn("b")],[turn("b"),turn("c")])).toBe(false);
  expect(historyRewritten([turn("a"),turn("b")],[turn("c"),turn("d")])).toBe(true);
 });
});
