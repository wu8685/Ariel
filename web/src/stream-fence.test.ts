import {it,expect} from "vitest";
import {StreamFence} from "./stream-fence";
it("rejects late snapshots and retired streams without resync or resurrecting replaced turns",()=>{
 const f=new StreamFence();expect(f.accept("watch","original",1)).toBe(true);expect(f.accept("watch","original",2)).toBe(true);expect(f.accept("watch","original",1)).toBe(false);
 expect(f.accept("watch","replacement",1)).toBe(true);expect(f.accept("watch","original",99)).toBe(false);expect(f.accept("new-watch","original",1)).toBe(true);
});
it("allows only the current seq for an explicit freshness recovery and keeps rollback fenced",()=>{
 const f=new StreamFence();expect(f.accept("watch","original",2)).toBe(true);
 expect(f.accept("watch","original",2)).toBe(false);
 expect(f.accept("watch","original",2,true)).toBe(true);
 expect(f.accept("watch","original",1,true)).toBe(false);
 expect(f.accept("watch","replacement",1)).toBe(true);
 expect(f.accept("watch","original",99,true)).toBe(false);
 expect(f.accept("watch","cache-pending",1,true)).toBe(false);
});
it("fences stale status without advancing content lineage or suppressing real failures",()=>{
 const f=new StreamFence();f.accept("watch","old",2);f.accept("watch","new",3);
 expect(f.acceptStatus("watch","old",99)).toBe(false);
 expect(f.acceptStatus("watch","new",2)).toBe(false);
 expect(f.acceptStatus("watch","new",3)).toBe(true);
 expect(f.acceptStatus("watch","new",4)).toBe(true);
 expect(f.accept("watch","new",4)).toBe(true);
 expect(f.acceptStatus("watch","next",1)).toBe(true);
 expect(f.accept("watch","new",5)).toBe(true);
 expect(f.acceptStatus("cold",undefined,undefined)).toBe(true);
 expect(f.acceptStatus("watch",undefined,undefined)).toBe(true);
});
