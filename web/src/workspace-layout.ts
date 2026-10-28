import {useCallback,useEffect,useRef,useState} from "react";

const wideQuery="(min-width: 1200px)";
const isWide=()=>typeof window.matchMedia==="function"?window.matchMedia(wideQuery).matches:window.innerWidth>=1200;

/** Layout preferences are UI state, independent of thread/file lifetime. */
export function useWorkspaceLayout(){
  const [wide,setWide]=useState(isWide);
  const mode=useRef(wide);
  const [dockOpen,setDockOpen]=useState(true);
  const [drawerOpen,setDrawerOpen]=useState(!wide);
  useEffect(()=>{
    const media=typeof window.matchMedia==="function"?window.matchMedia(wideQuery):null;
    const update=()=>{
      const next=media?media.matches:window.innerWidth>=1200;
      if(next===mode.current)return;
      mode.current=next;
      setDrawerOpen(false);
      setWide(next);
    };
    update();
    if(media)media.addEventListener("change",update);
    else window.addEventListener("resize",update);
    return ()=>{if(media)media.removeEventListener("change",update);else window.removeEventListener("resize",update)};
  },[]);
  const setSidebarOpen=useCallback((open:boolean)=>{
    if(wide)setDockOpen(open);else setDrawerOpen(open);
  },[wide]);
  const afterThreadSelection=useCallback(()=>setDrawerOpen(false),[]);
  return {wide,sidebarVisible:wide?dockOpen:drawerOpen,setSidebarOpen,afterThreadSelection};
}
