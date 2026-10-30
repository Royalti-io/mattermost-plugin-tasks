
import {api} from 'api';
import type {PatchParams} from 'api';
import {dateInputToDueAt, dueAtToDateInput, postPermalink} from 'format';
import React, {useState} from 'react';
import {PRIORITIES, STATUSES} from 'taskTypes';
import type {Priority, Status, Task} from 'taskTypes';

import AssigneePicker from './AssigneePicker';

type Props = {
    task: Task;
    currentUserId: string;
    replace: (task: Task) => void;
    reload: () => void;
    onError: (message: string) => void;
    onDeleted?: () => void;
};

// The editable body of a task: fields, assignment, checklist and delete.
// Shared by the list panel (inline) and the board (in a dialog).
export default function TaskDetail({task, currentUserId, replace, reload, onError, onDeleted}: Props) {
    const [newItem, setNewItem] = useState('');

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

    const deleteTask = async () => {
        try {
            await api.deleteTask(task.id);
            onDeleted?.();
        } catch (err) {
            onError(err instanceof Error ? err.message : 'Something went wrong');
        }
        reload();
    };

    const setAssignees = (next: string[]) => {
        replace({...task, assignees: next});
        patch({assignees: next});
    };

    return (
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
            {task.created_by === currentUserId ? (
                <label className='rt-inline'>
                    <input
                        type='checkbox'
                        checked={task.private}
                        onChange={(e) => patch({private: e.target.checked})}
                    />
                    {'Private: only you and the people assigned can see it'}
                </label>
            ) : null}
            <div className='rt-field'>
                {'Assignees'}
                <AssigneePicker
                    channelId={task.channel_id}
                    assignees={task.assignees}
                    currentUserId={currentUserId}
                    onChange={setAssignees}
                />
            </div>
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
                    onClick={deleteTask}
                >{'Delete task'}</button>
            </div>
        </div>
    );
}
