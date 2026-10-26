import {it,expect} from "vitest";
import {StreamFence} from "./stream-fence";
it("rejects late snapshots and retired streams without resync or resurrecting replaced turns",()=>{
 const f=new StreamFence();expect(f.accept("watch","original",1)).toBe(true);expect(f.accept("watch","original",2)).toBe(true);expect(f.accept("watch","original",1)).toBe(false);
 expect(f.accept("watch","replacement",1)).toBe(true);expect(f.accept("watch","original",99)).toBe(false);expect(f.accept("new-watch","original",1)).toBe(true);
});
