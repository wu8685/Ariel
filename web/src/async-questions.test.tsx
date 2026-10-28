// @vitest-environment jsdom
import {afterEach,expect,it,vi} from "vitest";
import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {AsyncQuestionCards,parseQuestionSnapshot,QuestionMemory,type AsyncQuestion} from "./async-questions";
afterEach(cleanup);
const question:AsyncQuestion={id:"question",fingerprint:"fingerprint",turnId:"turn",itemId:"message",index:0,title:"Choose color",options:["Blue","Green"],status:"open"};
it("shows native question cards even without pending interactions and never submits by selecting",()=>{
	const send=vi.fn(),memory=new QuestionMemory();
	render(<AsyncQuestionCards questions={[question]} writable memory={memory} scope="thread" disabledReason="" onRespond={send}/>);
	fireEvent.click(screen.getByRole("radio",{name:"Blue"}));expect(send).not.toHaveBeenCalled();
	fireEvent.click(screen.getByRole("button",{name:"发送回答"}));expect(send).toHaveBeenCalledWith(question,"Blue");
});
it("unverified profiles display read-only questions without blocking the ordinary composer",()=>{
	render(<><AsyncQuestionCards questions={[question]} writable={false} memory={new QuestionMemory()} scope="thread" disabledReason=""/><textarea aria-label="ordinary composer"/></>);
	expect(screen.getByRole("radio",{name:"Blue"}).matches(":disabled")).toBe(true);
	expect(screen.getByText(/尚未通过异步回答兼容性验证/)).toBeTruthy();
	expect((screen.getByRole("textbox",{name:"ordinary composer"}) as HTMLTextAreaElement).disabled).toBe(false);
});
it("skip is local and reversible; question draft survives remount but not fingerprint changes",()=>{
	const memory=new QuestionMemory(),send=vi.fn();
	const props={questions:[question],writable:true,memory,scope:"thread",disabledReason:"",onRespond:send};
	const ui=render(<AsyncQuestionCards {...props}/>);
	fireEvent.click(screen.getByRole("radio",{name:"其他"}));fireEvent.change(screen.getByRole("textbox",{name:"回答：Choose color"}),{target:{value:"my color"}});
	fireEvent.click(screen.getByRole("button",{name:"跳过"}));expect(send).not.toHaveBeenCalled();
	fireEvent.click(screen.getByRole("button",{name:"重新展开问题"}));ui.unmount();
	const second=render(<AsyncQuestionCards {...props}/>);expect((screen.getByRole("textbox",{name:"回答：Choose color"}) as HTMLTextAreaElement).value).toBe("my color");
	second.rerender(<AsyncQuestionCards {...props} questions={[{...question,fingerprint:"changed"}]}/>);expect(screen.queryByDisplayValue("my color")).toBeNull();
});
it("rejects oversized or malformed optional sidecars locally",()=>{
	expect(parseQuestionSnapshot({version:1,writable:false,questions:[question],warnings:[]})?.questions).toHaveLength(1);
	expect(parseQuestionSnapshot({version:1,questions:"invalid"})).toBeNull();
	expect(parseQuestionSnapshot({version:1,writable:false,questions:[{...question,title:"x".repeat(2049)}],warnings:[]})).toBeNull();
});
it("keeps confirmed answers when an older partial item page is merged",()=>{
 const memory=new QuestionMemory(),turns=[{turnId:"turn",items:[{itemId:"message"}]}];
 memory.receive("scope",{version:1,writable:false,questions:[{...question,status:"answered",answer:"Blue"}],warnings:[]},turns);
 expect(memory.receive("scope",{version:1,writable:false,questions:[{...question,status:"unverified"}],warnings:[]},turns,true)).toBe(true);
 expect(memory.forTurn("scope","turn")?.questions[0].answer).toBe("Blue");
});
it("bounds invalid optional-extension notices across visited conversations",()=>{
 const memory=new QuestionMemory();for(let i=0;i<600;i++)memory.receive(`scope-${i}`,{},[]);
 expect(memory.invalid("scope-0")).toBe(false);expect(memory.invalid("scope-599")).toBe(true);
});
