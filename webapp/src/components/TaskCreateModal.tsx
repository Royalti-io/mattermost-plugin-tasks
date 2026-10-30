
import {api} from 'api';
import {getState} from 'bridge';
import {useChannelChoices} from 'channels';
import {dateInputToDueAt, postPermalink, titleFromMessage} from 'format';
import React, {useEffect, useState} from 'react';
import {useSelector} from 'react-redux';
import {notifyTasksChanged} from 'taskEvents';
import {PRIORITIES} from 'taskTypes';
import type {Priority} from 'taskTypes';

import type {GlobalState} from '@mattermost/types/store';

import AssigneePicker from './AssigneePicker';

type Prefill = {
    key: string;

    // Set when the task is created from a message; the task then links back to it.
    postId?: string;
    channelId: string;
    title: string;
    description: string;
};

let current: Prefill | null = null;
let counter = 0;
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
        key: `post-${post.id}`,
        postId: post.id,
        channelId: post.channel_id,
        title: titleFromMessage(post.message),
        description: `${post.message}\n\n${link}`.slice(0, 10000),
    });
}

// Called from the sidebar "+" menu: a blank task, starting in the open channel.
export function openNewTask(): void {
    setCurrent({
        key: `new-${++counter}`,
        channelId: getState()?.entities.channels.currentChannelId || '',
        title: '',
        description: '',
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

export default function TaskCreateModal() {
    const prefill = useCurrent();
    if (!prefill) {
        return null;
    }

    // Keyed so reopening starts from fresh fields.
    return (
        <ModalBody
            key={prefill.key}
            prefill={prefill}
        />
    );
}

function ModalBody({prefill}: {prefill: Prefill}) {
    const currentUserId = useSelector((s: GlobalState) => s.entities.users.currentUserId);
    const choices = useChannelChoices(prefill.channelId);
    const fromPost = Boolean(prefill.postId);

    // From a message the channel is fixed; otherwise start in the open channel, or the first available.
    const [channelId, setChannelId] = useState(() => (
        choices.some((c) => c.id === prefill.channelId) ? prefill.channelId : (choices[0]?.id || prefill.channelId)
    ));
    const [title, setTitle] = useState(prefill.title);
    const [description, setDescription] = useState(prefill.description);
    const [priority, setPriority] = useState<Priority>('normal');
    const [due, setDue] = useState('');
    const [assignees, setAssignees] = useState<string[]>([currentUserId]);
    const [notify, setNotify] = useState(true);

    // The checkbox starts from the user's saved default. Until they touch it nothing
    // is sent, so the server applies that default itself.
    const [privateByDefault, setPrivateByDefault] = useState(false);
    const [privateChoice, setPrivateChoice] = useState<boolean | null>(null);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const heading = fromPost ? 'Create task from message' : 'Create a task';
    const noChannel = !fromPost && choices.length === 0;
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

    useEffect(() => {
        let cancelled = false;
        api.getPrefs().then((prefs) => {
            if (!cancelled) {
                setPrivateByDefault(prefs.private_by_default);
            }
        }).catch(() => {
            // Keep the shared default; the server applies the real one on submit.
        });
        return () => {
            cancelled = true;
        };
    }, []);

    const isPrivate = privateChoice ?? privateByDefault;

    const changeChannel = (id: string) => {
        setChannelId(id);

        // Other people may not be in the new channel, and the server only accepts members.
        setAssignees([currentUserId]);
    };

    const canSubmit = Boolean(title.trim()) && Boolean(channelId) && !noChannel && !saving;

    const submit = async () => {
        if (!canSubmit) {
            return;
        }
        setSaving(true);
        setError(null);
        try {
            await api.createTask({
                channel_id: channelId,
                title: title.trim(),
                description,
                priority,
                due_at: dateInputToDueAt(due),
                assignees,
                ...(privateChoice === null ? {} : {private: privateChoice}),
                ...(fromPost ? {post_id: prefill.postId, notify_thread: notify} : {}),
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
                aria-label={heading}
            >
                <h2 className='rt-modal__title'>{heading}</h2>
                {fromPost ? null : (
                    <label className='rt-field'>
                        {'Channel'}
                        <select
                            className='rt-input'
                            value={channelId}
                            onChange={(e) => changeChannel(e.target.value)}
                        >
                            {choices.map((c) => (
                                <option
                                    key={c.id}
                                    value={c.id}
                                >{c.label}</option>
                            ))}
                        </select>
                    </label>
                )}
                {noChannel ? <div className='rt-error'>{'Join a channel to create tasks.'}</div> : null}
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
                <label className='rt-field'>
                    {'Description'}
                    <textarea
                        className='rt-input'
                        rows={3}
                        maxLength={10000}
                        value={description}
                        onChange={(e) => setDescription(e.target.value)}
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
                <div className='rt-field'>
                    {'Assignees'}
                    <AssigneePicker
                        key={channelId}
                        channelId={channelId}
                        assignees={assignees}
                        currentUserId={currentUserId}
                        onChange={setAssignees}
                    />
                </div>
                <label className='rt-inline'>
                    <input
                        type='checkbox'
                        checked={isPrivate}
                        onChange={(e) => setPrivateChoice(e.target.checked)}
                    />
                    {'Private: only you and the people assigned can see it'}
                </label>
                {fromPost && !isPrivate ? (
                    <label className='rt-inline'>
                        <input
                            type='checkbox'
                            checked={notify}
                            onChange={(e) => setNotify(e.target.checked)}
                        />
                        {'Reply in the thread'}
                    </label>
                ) : null}
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
                        disabled={!canSubmit}
                        onClick={submit}
                    >{saving ? 'Creating…' : 'Create task'}</button>
                </div>
            </div>
        </div>
    );
}
