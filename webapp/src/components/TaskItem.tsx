import {api} from 'api';
import type {PatchParams} from 'api';
import {dateInputToDueAt, dueAtToDateInput, dueInfo, postPermalink} from 'format';
import React, {useState} from 'react';
import {PRIORITIES, STATUSES} from 'taskTypes';
import type {Priority, Status, Task} from 'taskTypes';

type Props = {
    task: Task;
    channelName?: string;
    currentUserId: string;
    userName: (id: string) => string;
    replace: (task: Task) => void;
    reload: () => void;
    onError: (message: string) => void;
};

export default function TaskItem({task, channelName, currentUserId, userName, replace, reload, onError}: Props) {
    const [open, setOpen] = useState(false);
    const [newItem, setNewItem] = useState('');
    const done = task.status === 'done';
    const due = dueInfo(task.due_at, done);
    const checked = task.checklist.filter((c) => c.done).length;

    // Runs an API call, swaps in the server's copy of the task, and reports failures.
    const run = async (fn: () => Promise<Task | void>) => {
        try {
            const result = await fn();
            if (result) {
                replace(result);
            } else {
                reload();
            }
        } catch (err) {
            onError(err instanceof Error ? err.message : 'Something went wrong');
            reload();
        }
    };

    const patch = (p: PatchParams) => run(() => api.updateTask(task.id, p));

    const toggleDone = () => {
        const next: Status = done ? 'todo' : 'done';
        replace({...task, status: next, completed_at: next === 'done' ? Date.now() : null});
        run(() => api.moveTask(task.id, next));
    };

    const toggleChecklist = async (id: string, value: boolean) => {
        replace({...task, checklist: task.checklist.map((c) => (c.id === id ? {...c, done: value} : c))});
        try {
            await api.updateChecklistItem(task.id, id, {done: value});
        } catch (err) {
            onError(err instanceof Error ? err.message : 'Something went wrong');
            reload();
        }
    };

    const addChecklist = async () => {
        const text = newItem.trim();
        if (!text) {
            return;
        }
        setNewItem('');
        try {
            await api.addChecklistItem(task.id, text);
        } catch (err) {
            onError(err instanceof Error ? err.message : 'Something went wrong');
        }
        reload();
    };

    const assignedToMe = task.assignees.includes(currentUserId);
    const toggleAssignMe = () => patch({
        assignees: assignedToMe ? task.assignees.filter((a) => a !== currentUserId) : [...task.assignees, currentUserId],
    });

    return (
        <li className={`rt-task${done ? ' rt-task--done' : ''}`}>
            <div className='rt-task__row'>
                <input
                    type='checkbox'
                    className='rt-task__check'
                    checked={done}
                    onChange={toggleDone}
                    aria-label={done ? 'Mark as not done' : 'Mark as done'}
                />
                <button
                    type='button'
                    className='rt-task__title'
                    onClick={() => setOpen(!open)}
                    aria-expanded={open}
                >
                    {task.title}
                </button>
                {task.priority === 'high' || task.priority === 'critical' ? (
                    <span className={`rt-chip rt-chip--${task.priority}`}>{task.priority}</span>
                ) : null}
            </div>
            <div className='rt-task__meta'>
                {due ? <span className={`rt-meta${due.overdue ? ' rt-meta--overdue' : ''}${due.soon ? ' rt-meta--soon' : ''}`}>{due.text}</span> : null}
                {task.status !== 'todo' && task.status !== 'done' ? <span className='rt-meta'>{STATUSES.find((s) => s.key === task.status)?.label}</span> : null}
                {task.assignees.length ? <span className='rt-meta'>{task.assignees.map((a) => '@' + userName(a)).join(', ')}</span> : null}
                {channelName ? <span className='rt-meta'>{'~' + channelName}</span> : null}
                {task.checklist.length ? <span className='rt-meta'>{`${checked}/${task.checklist.length}`}</span> : null}
                {task.labels.map((l) => (
                    <span
                        key={l}
                        className='rt-chip'
                    >{l}</span>
                ))}
            </div>
            {open ? (
                <div className='rt-detail'>
                    <input
                        className='rt-input'
                        defaultValue={task.title}
                        maxLength={200}
                        aria-label='Title'
                        onBlur={(e) => {
                            const v = e.target.value.trim();
                            if (v && v !== task.title) {
                                patch({title: v});
                            } else {
                                e.target.value = task.title;
                            }
                        }}
                    />
                    <textarea
                        className='rt-input'
                        rows={3}
                        defaultValue={task.description}
                        placeholder='Description'
                        aria-label='Description'
                        onBlur={(e) => {
                            if (e.target.value !== task.description) {
                                patch({description: e.target.value});
                            }
                        }}
                    />
                    <div className='rt-detail__fields'>
                        <label>
                            {'Status'}
                            <select
                                className='rt-input'
                                value={task.status}
                                onChange={(e) => run(() => api.moveTask(task.id, e.target.value as Status))}
                            >
                                {STATUSES.map((s) => (
                                    <option
                                        key={s.key}
                                        value={s.key}
                                    >{s.label}</option>
                                ))}
                            </select>
                        </label>
                        <label>
                            {'Priority'}
                            <select
                                className='rt-input'
                                value={task.priority}
                                onChange={(e) => patch({priority: e.target.value as Priority})}
                            >
                                {PRIORITIES.map((p) => (
                                    <option
                                        key={p.key}
                                        value={p.key}
                                    >{p.label}</option>
                                ))}
                            </select>
                        </label>
                        <label>
                            {'Due'}
                            <input
                                type='date'
                                className='rt-input'
                                value={dueAtToDateInput(task.due_at)}
                                onChange={(e) => patch({due_at: dateInputToDueAt(e.target.value)})}
                            />
                        </label>
                    </div>
                    <label className='rt-inline'>
                        <input
                            type='checkbox'
                            checked={assignedToMe}
                            onChange={toggleAssignMe}
                        />
                        {'Assigned to me'}
                    </label>
                    <ul className='rt-checklist'>
                        {task.checklist.map((c) => (
                            <li key={c.id}>
                                <label className='rt-inline'>
                                    <input
                                        type='checkbox'
                                        checked={c.done}
                                        onChange={(e) => toggleChecklist(c.id, e.target.checked)}
                                    />
                                    <span className={c.done ? 'rt-struck' : ''}>{c.text}</span>
                                </label>
                                <button
                                    type='button'
                                    className='rt-link'
                                    aria-label={`Remove ${c.text}`}
                                    onClick={() => run(() => api.deleteChecklistItem(task.id, c.id))}
                                >{'×'}</button>
                            </li>
                        ))}
                    </ul>
                    <input
                        className='rt-input'
                        value={newItem}
                        placeholder='Add a checklist item'
                        maxLength={500}
                        aria-label='New checklist item'
                        onChange={(e) => setNewItem(e.target.value)}
                        onKeyDown={(e) => {
                            if (e.key === 'Enter') {
                                addChecklist();
                            }
                        }}
                    />
                    <div className='rt-detail__actions'>
                        {task.post_id ? (
                            <a
                                className='rt-link'
                                href={postPermalink(window.location.origin, task.post_id)}
                            >{'Open source message'}</a>
                        ) : <span/>}
                        <button
                            type='button'
                            className='rt-link rt-link--danger'
                            onClick={() => run(() => api.deleteTask(task.id))}
                        >{'Delete task'}</button>
                    </div>
                </div>
            ) : null}
        </li>
    );
}
