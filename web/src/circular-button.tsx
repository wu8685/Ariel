import type { ButtonHTMLAttributes, ReactNode } from "react";
export function CircleButton({children,className="",type="button",...props}:ButtonHTMLAttributes<HTMLButtonElement>){
 return <button {...props} type={type} className={`icon-control circle-control ${className}`}><span className="circle-visual" aria-hidden="true">{children}</span></button>;
}
function Glyph({children}: {children:ReactNode}) {return <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">{children}</svg>;}
export function CloseGlyph(){return <Glyph><path d="m6 6 12 12M18 6 6 18"/></Glyph>;}
export function BackGlyph(){return <Glyph><path d="m15 5-7 7 7 7"/></Glyph>;}
export function RefreshGlyph(){return <Glyph><path d="M20 10a8 8 0 1 0-1 7M20 4v6h-6"/></Glyph>;}
