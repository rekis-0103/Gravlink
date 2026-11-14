import { FormEvent, KeyboardEvent, useEffect, useRef, useState } from 'react';
import { api } from '../api';
import type { ModelOption } from '../types';

interface SpeechRecognitionLike {
  lang: string;
  interimResults: boolean;
  continuous: boolean;
  onresult: ((ev: { results: ArrayLike<ArrayLike<{ transcript: string }>> }) => void) | null;
  onerror: (() => void) | null;
  onend: (() => void) | null;
  start: () => void;
  stop: () => void;
}

interface PromptComposerProps {
  conversationId: string;
  onSent: (targetConvId: string) => void;
  onNewChat: () => void;
}

export function PromptComposer({ conversationId, onSent, onNewChat }: PromptComposerProps) {
  const [draft, setDraft] = useState('');
  const [mode, setMode] = useState<'auto' | 'plan'>('auto');
  const [model, setModel] = useState('');
  const [models, setModels] = useState<ModelOption[]>([]);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [listening, setListening] = useState(false);

  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const recognitionRef = useRef<SpeechRecognitionLike | null>(null);

  const speechCtor =
    typeof window !== 'undefined'
      ? ((window as unknown as Record<string, unknown>).SpeechRecognition ||
          (window as unknown as Record<string, unknown>).webkitSpeechRecognition)
      : undefined;

  useEffect(() => {
    let active = true;
    api
      .models()
      .then((res) => {
        if (active && res.models?.length) {
          setModels(res.models);
        }
      })
      .catch(() => {
        /* ignore offline models list */
      });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    return () => {
      recognitionRef.current?.stop();
    };
  }, []);

  // Auto-grow textarea up to CSS max-height
  useEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, 136)}px`;
  }, [draft]);

  async function submitPrompt() {
    const text = draft.trim();
    if (!text || sending) return;
    setSending(true);
    setError(null);
    try {
      const target = conversationId || 'new';
      const res = await api.sendPrompt(target, {
        prompt: text,
        mode,
        model: model || undefined,
      });
      setDraft('');
      onSent(res.conversation_id || conversationId);
    } catch (err) {
      setError((err as Error).message || 'Failed to send prompt');
    } finally {
      setSending(false);
    }
  }

  function handleFormSubmit(ev: FormEvent) {
    ev.preventDefault();
    void submitPrompt();
  }

  // Mobile Enter inserts newline; Ctrl/Cmd+Enter sends on physical keyboards.
  function handleKeyDown(ev: KeyboardEvent<HTMLTextAreaElement>) {
    if (ev.key === 'Enter' && (ev.ctrlKey || ev.metaKey)) {
      ev.preventDefault();
      void submitPrompt();
    }
  }

  function toggleVoice() {
    if (!speechCtor) return;
    if (listening && recognitionRef.current) {
      recognitionRef.current.stop();
      setListening(false);
      return;
    }

    const Ctor = speechCtor as new () => SpeechRecognitionLike;
    const rec = new Ctor();
    rec.lang = navigator.language || 'en-US';
    rec.interimResults = false;
    rec.continuous = false;
    rec.onresult = (event) => {
      const parts: string[] = [];
      for (let i = 0; i < event.results.length; i++) {
        const alt = event.results[i]?.[0];
        if (alt?.transcript) parts.push(alt.transcript.trim());
      }
      const spoken = parts.join(' ').trim();
      if (spoken) {
        setDraft((prev) => (prev.trim() ? `${prev.trimEnd()} ${spoken}` : spoken));
      }
    };
    rec.onerror = () => setListening(false);
    rec.onend = () => setListening(false);
    recognitionRef.current = rec;
    setListening(true);
    rec.start();
  }

  return (
    <form className="composer-dock" onSubmit={handleFormSubmit}>
      <div className="composer-meta-row">
        <button
          type="button"
          className="compact"
          onClick={onNewChat}
          title="Start a brand new conversation"
        >
          + New
        </button>

        <label htmlFor="composer-mode" className="visually-hidden">
          Agent mode
        </label>
        <select
          id="composer-mode"
          className="composer-select"
          value={mode}
          onChange={(e) => setMode(e.target.value as 'auto' | 'plan')}
          aria-label="Agent mode"
        >
          <option value="auto">Mode: Auto</option>
          <option value="plan">Mode: Plan</option>
        </select>

        {models.length > 0 && (
          <>
            <label htmlFor="composer-model" className="visually-hidden">
              Model
            </label>
            <select
              id="composer-model"
              className="composer-select"
              value={model}
              onChange={(e) => setModel(e.target.value)}
              aria-label="Model"
            >
              <option value="">Default model</option>
              {models.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.label}
                </option>
              ))}
            </select>
          </>
        )}
      </div>

      {error && <div className="composer-error">{error}</div>}

      <div className="composer-box">
        <label htmlFor="prompt-input" className="visually-hidden">
          Message Antigravity
        </label>
        <textarea
          ref={textareaRef}
          id="prompt-input"
          className="composer-textarea"
          rows={1}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={
            conversationId ? 'Reply to agent (Enter = newline)...' : 'Start a new prompt...'
          }
          disabled={sending}
        />

        {Boolean(speechCtor) && (
          <button
            type="button"
            className={`composer-icon-btn ${listening ? 'listening' : ''}`}
            onClick={toggleVoice}
            aria-label={listening ? 'Stop voice dictation' : 'Start voice dictation'}
            title={listening ? 'Stop voice input' : 'Voice dictation'}
          >
            {listening ? '⏹' : '🎤'}
          </button>
        )}

        <button
          type="submit"
          className="primary composer-icon-btn"
          disabled={sending || !draft.trim()}
          aria-label="Send prompt"
          title="Send prompt (Ctrl/Cmd+Enter)"
        >
          {sending ? '…' : '↑'}
        </button>
      </div>
    </form>
  );
}
