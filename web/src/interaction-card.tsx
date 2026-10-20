import { useState } from "react";
import { answersForSubmission } from "./interaction";
import type { Interaction } from "./generated/protocol";

type InteractionCardProps = {
  card: Interaction;
  values: Record<string, string>;
  onChange: (id: string, value: string) => void;
  onRespond: (decision: string) => void;
  disabled: boolean;
  submitting?: boolean;
};

type OptionView = { label: string; description?: string };

function optionView(option: string | OptionView): OptionView {
  return typeof option === "string" ? { label: option } : option;
}

function cardTitle(kind: Interaction["kind"]): string {
  return kind === "command_approval" ? "等待命令审批"
    : kind === "file_approval" ? "等待文件变更审批"
      : kind === "permission_request" ? "等待权限请求"
        : kind === "user_input" ? "Codex 有一个问题"
          : "暂不支持的交互";
}

function decisionLabel(card: Interaction, decision: string, submitting: boolean): string {
  if (decision === "answer" && submitting) return "正在提交…";
  if (decision === "accept_once" && card.kind === "permission_request") return "仅本轮按原请求授权";
  return ({ accept_once: "仅本次允许", deny: "拒绝", deny_and_stop: "拒绝并停止", answer: "提交回答" } as Record<string, string>)[decision] || decision;
}

export function InteractionCard({ card, values, onChange, onRespond, disabled, submitting = false }: InteractionCardProps) {
  const [customQuestions, setCustomQuestions] = useState<Set<string>>(() => new Set());

  return <section className="interaction-card">
    <span className="eyebrow">NEEDS YOUR INPUT</span>
    <h3>{cardTitle(card.kind)}</h3>
    <p>{card.prompt}</p>
    {card.questions?.map(question => {
      const options = (question.options || []).map(optionView);
      const optionLabels = options.map(option => option.label);
      const current = values[question.id] || "";
      const custom = customQuestions.has(question.id) || (!!current && !optionLabels.includes(current));
      const selectOption = (label: string) => {
        setCustomQuestions(ids => {
          const next = new Set(ids);
          next.delete(question.id);
          return next;
        });
        onChange(question.id, label);
      };
      const selectCustom = () => {
        setCustomQuestions(ids => new Set(ids).add(question.id));
        if (optionLabels.includes(current)) onChange(question.id, "");
      };
      return <fieldset className="question" key={question.id} disabled={disabled}>
        <legend>
          {question.header && <span className="question-header">{question.header}</span>}
          <span className="question-prompt">{question.question}</span>
        </legend>
        {options.length > 0 ? <div className="question-options">
          {options.map(option => <label className={`question-option ${!custom && current === option.label ? "selected" : ""}`} key={option.label}>
            <input aria-label={option.label} type="radio" name={`${card.interactionId}-${question.id}`} checked={!custom && current === option.label} onChange={() => selectOption(option.label)} />
            <span className="question-option-copy"><strong>{option.label}</strong>{option.description && <small>{option.description}</small>}</span>
          </label>)}
          <label className={`question-option ${custom ? "selected" : ""}`}>
            <input aria-label="其他" type="radio" name={`${card.interactionId}-${question.id}`} checked={custom} onChange={selectCustom} />
            <span className="question-option-copy"><strong>其他</strong><small>输入自定义回答</small></span>
          </label>
        </div> : null}
        {(options.length === 0 || custom) && <textarea aria-label={options.length ? "自定义回答" : question.question} value={current} onChange={event => onChange(question.id, event.target.value)} placeholder="输入回答" rows={3} />}
      </fieldset>;
    })}
    <div className="interaction-actions">{card.availableDecisions.map(decision => <button key={decision} className={decision === "accept_once" || decision === "answer" ? "primary" : "secondary"} disabled={disabled || (decision === "answer" && !answersForSubmission(card, values))} onClick={() => onRespond(decision)}>{decisionLabel(card, decision, submitting)}</button>)}</div>
  </section>;
}
