
import {api} from 'api';
import {DONE_VISIBLE, dropPosition, groupByStatus, insertionIndex} from 'board';
import {dueInfo} from 'format';
import {useTasks} from 'hooks';
import React, {useEffect, useMemo, useRef, useState} from 'react';
import {useSelector} from 'react-redux';
import {STATUSES} from 'taskTypes';
import type {Status, Task} from 'taskTypes';
import {useUserNames} from 'userNames';

import type {GlobalState} from '@mattermost/types/store';

import TaskDetail from './TaskDetail';

type Scope = 'channel' | 'mine' | 'all';

const STORAGE_SCOPE = 'rt-board-scope';
const STORAGE_CHANNEL = 'rt-board-channel';

// Remembered per browser only; the page works the same when storage is unavailable.
function readPref(key: string): string | null {
    try {
        return window.localStorage.getItem(key);
    } catch {
        return null;
    }
}

function writePref(key: string, value: string): void {
    try {
        window.localStorage.setItem(key, value);
    } catch {
        // Ignore: a preference that does not persist is not an error.
    }
}

type DropTarget = {status: Status; index: number};

export default function BoardPage() {
    const currentUserId = useSelector((s: GlobalState) => s.entities.users.currentUserId);
    const currentChannelId = useSelector((s: GlobalState) => s.entities.channels.currentChannelId);
    const channels = useSelector((s: GlobalState) => s.entities.channels.channels);
    const myMembers = useSelector((s: GlobalState) => s.entities.channels.myMembers);

    // Public and private channels the user belongs to; DMs have no useful name to pick by.
    const channelOptions = useMemo(() => Object.values(channels).
        filter((c) => myMembers[c.id] && (c.type === 'O' || c.type === 'P') && c.delete_at === 0).
        sort((a, b) => (a.display_name || a.name).localeCompare(b.display_name || b.name)), [channels, myMembers]);

    const [scope, setScope] = useState<Scope>(() => {
        const saved = readPref(STORAGE_SCOPE);
        return saved === 'mine' || saved === 'all' ? saved : 'channel';
    });
    const [pickedChannel, setPickedChannel] = useState<string>(() => readPref(STORAGE_CHANNEL) || '');
    const [search, setSearch] = useState('');
    const [query, setQuery] = useState('');
    const [selectedId, setSelectedId] = useState<string | null>(null);
    const [error, setError] = useState<string | null>(null);
    const [showAllDone, setShowAllDone] = useState(false);
    const [dragId, setDragId] = useState<string | null>(null);
    const [drop, setDrop] = useState<DropTarget | null>(null);
    const [addingIn, setAddingIn] = useState<Status | null>(null);
    const [newTitle, setNewTitle] = useState('');
    const colRefs = useRef<Partial<Record<Status, HTMLDivElement | null>>>({});

    // The channel in use: the remembered pick if it is still available, else the open channel, else the first.
    const channelId = useMemo(() => {
        const ids = new Set(channelOptions.map((c) => c.id));
        if (pickedChannel && ids.has(pickedChannel)) {
            return pickedChannel;
        }
        if (ids.has(currentChannelId)) {
            return currentChannelId;
        }
        return channelOptions[0]?.id || '';
    }, [pickedChannel, currentChannelId, channelOptions]);

    useEffect(() => {
        const t = setTimeout(() => setQuery(search.trim()), 300);
        return () => clearTimeout(t);
    }, [search]);

    const {tasks, loading, error: loadError, reload, replace} = useTasks({
        channel_id: scope === 'channel' ? channelId : undefined,
        assignee: scope === 'mine' ? 'me' : undefined,
        q: query || undefined,
        limit: 500,
    }, scope !== 'channel' || channelId !== '');

    const groups = useMemo(() => groupByStatus(tasks), [tasks]);
    const selected = selectedId ? tasks.find((t) => t.id === selectedId) : undefined;

    const channelLabel = (id: string) => {
        const c = channels[id];
        return c ? (c.display_name || c.name) : undefined;
    };
    const userName = useUserNames(tasks.flatMap((t) => t.assignees));

    const changeScope = (next: Scope) => {
        setScope(next);
        writePref(STORAGE_SCOPE, next);
    };

    const changeChannel = (id: string) => {
        setPickedChannel(id);
        writePref(STORAGE_CHANNEL, id);
    };

    const moveTo = async (task: Task, status: Status, index: number) => {
        // Done is ordered by completion time, so a drop there never picks a position.
        const position = status === 'done' ? undefined : dropPosition(groups[status], task.id, index);
        if (status === task.status && position === task.position) {
            return;
        }
        replace({
            ...task,
            status,
            position: position ?? task.position,
            completed_at: status === 'done' ? (task.completed_at ?? Date.now()) : null,
        });
        try {
            replace(await api.moveTask(task.id, status, position));
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Could not move the task');
            reload();
        }
    };

    const indexAt = (status: Status, clientY: number): number => {
        const col = colRefs.current[status];
        if (!col) {
            return 0;
        }
        const mids = Array.from(col.querySelectorAll<HTMLElement>('[data-task-id]')).
            filter((el) => el.dataset.taskId !== dragId).
            map((el) => {
                const r = el.getBoundingClientRect();
                return r.top + (r.height / 2);
            });
        return insertionIndex(mids, clientY);
    };

    const addTask = async (status: Status) => {
        const title = newTitle.trim();
        if (!title || !channelId) {
            return;
        }
        setNewTitle('');
        setAddingIn(null);
        try {
            await api.createTask({channel_id: channelId, title, status});
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Could not create the task');
        }
        reload();
    };

    const shownError = error || loadError;
    const canAdd = scope === 'channel' && channelId !== '';

    return (
        <div className='rt-board'>
            <div className='rt-board__head'>
                <h1 className='rt-board__title'>{'Tasks'}</h1>
                <select
                    className='rt-input rt-board__ctl'
                    value={scope}
                    aria-label='Scope'
                    onChange={(e) => changeScope(e.target.value as Scope)}
                >
                    <option value='channel'>{'One channel'}</option>
                    <option value='mine'>{'Assigned to me'}</option>
                    <option value='all'>{'All my channels'}</option>
                </select>
                {scope === 'channel' ? (
                    <select
                        className='rt-input rt-board__ctl'
                        value={channelId}
                        aria-label='Channel'
                        onChange={(e) => changeChannel(e.target.value)}
                    >
                        {channelOptions.map((c) => (
                            <option
                                key={c.id}
                                value={c.id}
                            >{'~' + (c.display_name || c.name)}</option>
                        ))}
                    </select>
                ) : null}
                <input
                    className='rt-input rt-board__ctl'
                    type='search'
                    value={search}
                    placeholder='Search tasks'
                    aria-label='Search tasks'
                    onChange={(e) => setSearch(e.target.value)}
                />
            </div>
            {shownError ? (
                <div
                    className='rt-error rt-board__error'
                    role='alert'
                >{shownError}</div>
            ) : null}
            {scope === 'channel' && !channelId ? <div className='rt-empty'>{'Join a channel to see its tasks.'}</div> : null}
            <div className='rt-board__cols'>
                {STATUSES.map(({key, label}) => {
                    const all = groups[key];
                    const hidden = key === 'done' && !showAllDone ? Math.max(0, all.length - DONE_VISIBLE) : 0;
                    const column = hidden ? all.slice(0, DONE_VISIBLE) : all;
                    let adder: React.ReactNode = null;
                    if (canAdd && key !== 'done') {
                        adder = addingIn === key ? (
                            <input
                                className='rt-input rt-col__add'
                                value={newTitle}
                                maxLength={200}
                                placeholder='Task title, then Enter'
                                aria-label={`New task in ${label}`}
                                autoFocus={true}
                                onChange={(e) => setNewTitle(e.target.value)}
                                onBlur={() => {
                                    setAddingIn(null);
                                    setNewTitle('');
                                }}
                                onKeyDown={(e) => {
                                    if (e.key === 'Enter') {
                                        addTask(key);
                                    } else if (e.key === 'Escape') {
                                        setAddingIn(null);
                                        setNewTitle('');
                                    }
                                }}
                            />
                        ) : (
                            <button
                                type='button'
                                className='rt-link rt-col__addbtn'
                                onClick={() => setAddingIn(key)}
                            >{'+ Add task'}</button>
                        );
                    }
                    return (
                        <section
                            key={key}
                            className={`rt-col${drop?.status === key ? ' rt-col--over' : ''}`}
                            aria-label={label}
                            onDragOver={(e) => {
                                if (!dragId) {
                                    return;
                                }
                                e.preventDefault();
                                e.dataTransfer.dropEffect = 'move';
                                const index = indexAt(key, e.clientY);
                                if (drop?.status !== key || drop.index !== index) {
                                    setDrop({status: key, index});
                                }
                            }}
                            onDragLeave={(e) => {
                                if (!e.currentTarget.contains(e.relatedTarget as Node | null)) {
                                    setDrop(null);
                                }
                            }}
                            onDrop={(e) => {
                                e.preventDefault();
                                const task = tasks.find((t) => t.id === dragId);
                                const index = indexAt(key, e.clientY);
                                setDragId(null);
                                setDrop(null);
                                if (task) {
                                    moveTo(task, key, index);
                                }
                            }}
                        >
                            <header className='rt-col__head'>
                                <span>{label}</span>
                                <span className='rt-col__count'>{all.length}</span>
                            </header>
                            <div
                                className='rt-col__list'
                                ref={(el) => {
                                    colRefs.current[key] = el;
                                }}
                            >
                                {column.map((task, i) => (
                                    <React.Fragment key={task.id}>
                                        {drop?.status === key && drop.index === i && key !== 'done' ? <div className='rt-drop'/> : null}
                                        <Card
                                            task={task}
                                            dragging={dragId === task.id}
                                            channelName={scope === 'channel' ? undefined : channelLabel(task.channel_id)}
                                            userName={userName}
                                            onOpen={() => setSelectedId(task.id)}
                                            onDragStart={() => setDragId(task.id)}
                                            onDragEnd={() => {
                                                setDragId(null);
                                                setDrop(null);
                                            }}
                                        />
                                    </React.Fragment>
                                ))}
                                {drop?.status === key && key !== 'done' && drop.index >= column.length ? <div className='rt-drop'/> : null}
                                {!loading && !all.length ? <div className='rt-col__empty'>{'Nothing here'}</div> : null}
                            </div>
                            {hidden ? (
                                <button
                                    type='button'
                                    className='rt-link rt-col__more'
                                    onClick={() => setShowAllDone(true)}
                                >{`Show ${hidden} more`}</button>
                            ) : null}
                            {adder}
                        </section>
                    );
                })}
            </div>
            {selected ? (
                <TaskDialog
                    title={selected.title}
                    onClose={() => setSelectedId(null)}
                >
                    <TaskDetail
                        task={selected}
                        currentUserId={currentUserId}
                        replace={replace}
                        reload={reload}
                        onError={setError}
                        onDeleted={() => setSelectedId(null)}
                    />
                </TaskDialog>
            ) : null}
        </div>
    );
}

type CardProps = {
    task: Task;
    dragging: boolean;
    channelName?: string;
    userName: (id: string) => string;
    onOpen: () => void;
    onDragStart: () => void;
    onDragEnd: () => void;
};

function Card({task, dragging, channelName, userName, onOpen, onDragStart, onDragEnd}: CardProps) {
    const due = dueInfo(task.due_at, task.status === 'done');
    const checked = task.checklist.filter((c) => c.done).length;
    return (
        <div
            className={`rt-card${dragging ? ' rt-card--dragging' : ''}`}
            data-task-id={task.id}
            draggable={true}
            role='button'
            tabIndex={0}
            aria-label={task.title}
            onClick={onOpen}
            onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    onOpen();
                }
            }}
            onDragStart={(e) => {
                e.dataTransfer.effectAllowed = 'move';
                e.dataTransfer.setData('text/plain', task.id);
                onDragStart();
            }}
            onDragEnd={onDragEnd}
        >
            <div className='rt-card__title'>{task.title}</div>
            <div className='rt-task__meta rt-card__meta'>
                {task.priority === 'high' || task.priority === 'critical' ? (
                    <span className={`rt-chip rt-chip--${task.priority}`}>{task.priority}</span>
                ) : null}
                {due ? <span className={`rt-meta${due.overdue ? ' rt-meta--overdue' : ''}${due.soon ? ' rt-meta--soon' : ''}`}>{due.text}</span> : null}
                {task.assignees.length ? <span className='rt-meta'>{task.assignees.map((a) => '@' + userName(a)).join(', ')}</span> : null}
                {task.checklist.length ? <span className='rt-meta'>{`${checked}/${task.checklist.length}`}</span> : null}
                {channelName ? <span className='rt-meta'>{'~' + channelName}</span> : null}
                {task.labels.map((l) => (
                    <span
                        key={l}
                        className='rt-chip'
                    >{l}</span>
                ))}
            </div>
        </div>
    );
}

function TaskDialog({title, onClose, children}: {title: string; onClose: () => void; children: React.ReactNode}) {
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.key === 'Escape') {
                onClose();
            }
        };
        document.addEventListener('keydown', onKey);
        return () => document.removeEventListener('keydown', onKey);
    }, [onClose]);

    return (
        <div
            className='rt-backdrop'
            onMouseDown={(e) => {
                if (e.target === e.currentTarget) {
                    onClose();
                }
            }}
        >
            <div
                className='rt-modal rt-modal--wide'
                role='dialog'
                aria-modal='true'
                aria-label={title}
            >
                <div className='rt-modal__bar'>
                    <h2 className='rt-modal__title'>{title}</h2>
                    <button
                        type='button'
                        className='rt-link'
                        aria-label='Close'
                        onClick={onClose}
                    >{'×'}</button>
                </div>
                {children}
            </div>
        </div>
    );
}
