
import {api} from 'api';
import {dueInfo} from 'format';
import React, {useState} from 'react';
import {STATUSES} from 'taskTypes';
import type {Status, Task} from 'taskTypes';

import TaskDetail from './TaskDetail';

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
    const done = task.status === 'done';
    const due = dueInfo(task.due_at, done);
    const checked = task.checklist.filter((c) => c.done).length;

    const toggleDone = async () => {
        const next: Status = done ? 'todo' : 'done';
        replace({...task, status: next, completed_at: next === 'done' ? Date.now() : null});
        try {
            replace(await api.moveTask(task.id, next));
        } catch (err) {
            onError(err instanceof Error ? err.message : 'Something went wrong');
            reload();
        }
    };

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
                {task.private ? (
                    <span
                        className='rt-chip'
                        title='Only you and the people assigned can see this task'
                    >{'🔒 Private'}</span>
                ) : null}
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
                <TaskDetail
                    task={task}
                    currentUserId={currentUserId}
                    replace={replace}
                    reload={reload}
                    onError={onError}
                />
            ) : null}
        </li>
    );
}
