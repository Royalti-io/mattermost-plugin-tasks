
import {api} from 'api';
import {getState} from 'bridge';
import {dateInputToDueAt, postPermalink, titleFromMessage} from 'format';
import React, {useEffect, useState} from 'react';
import {useSelector} from 'react-redux';
import {notifyTasksChanged} from 'taskEvents';
import {PRIORITIES} from 'taskTypes';
import type {Priority} from 'taskTypes';

import type {GlobalState} from '@mattermost/types/store';

type Prefill = {postId: string; channelId: string; title: string; description: string};

let current: Prefill | null = null;
const listeners = new Set<() => void>();

function setCurrent(next: Prefill | null) {
    current = next;
    listeners.forEach((fn) => fn());
}

// Called from the post "..." menu. Reads the post from the host store.
export function openCreateFromPost(postId: string): void {
    const post = getState()?.entities.posts.posts[postId];
    if (!post) {
        return;
    }
    const link = postPermalink(window.location.origin, post.id);
    setCurrent({
        postId: post.id,
        channelId: post.channel_id,
        title: titleFromMessage(post.message),
        description: `${post.message}\n\n${link}`.slice(0, 10000),
    });
}

function useCurrent(): Prefill | null {
    const [value, setValue] = useState<Prefill | null>(current);
    useEffect(() => {
        const fn = () => setValue(current);
        listeners.add(fn);
        fn();
        return () => {
            listeners.delete(fn);
        };
    }, []);
    return value;
}

export default function CreateFromPostModal() {
    const prefill = useCurrent();
    if (!prefill) {
        return null;
    }

    // Keyed by post so reopening for another message starts from fresh fields.
    return (
        <ModalBody
            key={prefill.postId}
            prefill={prefill}
        />
    );
}

function ModalBody({prefill}: {prefill: Prefill}) {
    const currentUserId = useSelector((s: GlobalState) => s.entities.users.currentUserId);
    const [title, setTitle] = useState(prefill.title);
    const [priority, setPriority] = useState<Priority>('normal');
    const [due, setDue] = useState('');
    const [assignMe, setAssignMe] = useState(true);
    const [notify, setNotify] = useState(true);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const close = () => setCurrent(null);

    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.key === 'Escape') {
                close();
            }
        };
        document.addEventListener('keydown', onKey);
        return () => document.removeEventListener('keydown', onKey);
    }, []);

    const submit = async () => {
        if (!title.trim() || saving) {
            return;
        }
        setSaving(true);
        setError(null);
        try {
            await api.createTask({
                channel_id: prefill.channelId,
                title: title.trim(),
                description: prefill.description,
                priority,
                due_at: dateInputToDueAt(due),
                assignees: assignMe ? [currentUserId] : [],
                post_id: prefill.postId,
                notify_thread: notify,
            });
            notifyTasksChanged();
            close();
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Could not create the task');
            setSaving(false);
        }
    };

    return (
        <div
            className='rt-backdrop'
            onMouseDown={(e) => {
                if (e.target === e.currentTarget) {
                    close();
                }
            }}
        >
            <div
                className='rt-modal'
                role='dialog'
                aria-modal='true'
                aria-label='Create task from message'
            >
                <h2 className='rt-modal__title'>{'Create task from message'}</h2>
                <label className='rt-field'>
                    {'Title'}
                    <input
                        className='rt-input'
                        value={title}
                        maxLength={200}
                        autoFocus={true}
                        onChange={(e) => setTitle(e.target.value)}
                        onKeyDown={(e) => {
                            if (e.key === 'Enter') {
                                submit();
                            }
                        }}
                    />
                </label>
                <div className='rt-detail__fields'>
                    <label className='rt-field'>
                        {'Priority'}
                        <select
                            className='rt-input'
                            value={priority}
                            onChange={(e) => setPriority(e.target.value as Priority)}
                        >
                            {PRIORITIES.map((p) => (
                                <option
                                    key={p.key}
                                    value={p.key}
                                >{p.label}</option>
                            ))}
                        </select>
                    </label>
                    <label className='rt-field'>
                        {'Due'}
                        <input
                            type='date'
                            className='rt-input'
                            value={due}
                            onChange={(e) => setDue(e.target.value)}
                        />
                    </label>
                </div>
                <label className='rt-inline'>
                    <input
                        type='checkbox'
                        checked={assignMe}
                        onChange={(e) => setAssignMe(e.target.checked)}
                    />
                    {'Assign to me'}
                </label>
                <label className='rt-inline'>
                    <input
                        type='checkbox'
                        checked={notify}
                        onChange={(e) => setNotify(e.target.checked)}
                    />
                    {'Reply in the thread'}
                </label>
                {error ? (
                    <div
                        className='rt-error'
                        role='alert'
                    >{error}</div>
                ) : null}
                <div className='rt-modal__actions'>
                    <button
                        type='button'
                        className='rt-btn rt-btn--ghost'
                        onClick={close}
                    >{'Cancel'}</button>
                    <button
                        type='button'
                        className='rt-btn'
                        disabled={!title.trim() || saving}
                        onClick={submit}
                    >{saving ? 'Creating…' : 'Create task'}</button>
                </div>
            </div>
        </div>
    );
}
