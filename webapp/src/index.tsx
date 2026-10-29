// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {setStore} from 'bridge';
import manifest from 'manifest';
import {BOARD_PATH} from 'navigation';
import React from 'react';
import type {Store} from 'redux';
import {notifyTasksChanged} from 'taskEvents';

import type {GlobalState} from '@mattermost/types/store';

import BoardPage from 'components/BoardPage';
import TaskCreateModal, {openCreateFromPost, openNewTask} from 'components/TaskCreateModal';
import TasksPanel from 'components/TasksPanel';

import type {PluginRegistry} from 'types/mattermost-webapp';

import 'styles.scss';

const apiBase = `/plugins/${manifest.id}/api/v1`;

// Asks the server whether this user may see the plugin UI. Any failure means no UI,
// so a broken or unreachable plugin never affects other users' browsers.
async function uiEnabled(): Promise<boolean> {
    try {
        const resp = await fetch(`${apiBase}/access`, {credentials: 'same-origin'});
        if (!resp.ok) {
            return false;
        }
        const body = await resp.json();
        return body.ui_enabled === true;
    } catch {
        return false;
    }
}

const TasksIcon = () => (
    <svg
        width='18'
        height='18'
        viewBox='0 0 24 24'
        fill='none'
        stroke='currentColor'
        strokeWidth='2'
        aria-hidden='true'
    >
        <rect
            x='4'
            y='4'
            width='16'
            height='16'
            rx='2'
        />
        <path d='M8 12l3 3 5-6'/>
    </svg>
);

const BoardHeader = () => <span style={{fontWeight: 600}}>{'Tasks'}</span>;

export default class Plugin {
    public async initialize(registry: PluginRegistry, _store: Store<GlobalState>) {
        try {
            if (!(await uiEnabled())) {
                return;
            }

            setStore(_store);

            const {toggleRHSPlugin} = registry.registerRightHandSidebarComponent(TasksPanel, 'Tasks');
            registry.registerChannelHeaderButtonAction(
                <TasksIcon/>,
                () => _store.dispatch(toggleRHSPlugin as never),
                'Tasks',
                'Tasks',
            );

            registry.registerPostDropdownMenuAction({
                text: 'Create task',

                // Mattermost calls the action with the post id; the typings omit it.
                action: ((postId: string) => openCreateFromPost(postId)) as unknown as () => void,
                filter: () => true,
            });
            registry.registerRootComponent(TaskCreateModal);

            // The "+" menu at the top of the channel sidebar. Mattermost 12 has this hook but the
            // bundled typings do not, so it is called through a narrow type and feature-detected.
            const sidebarMenu = registry as unknown as {
                registerSidebarBrowseOrAddChannelMenuAction?: (text: string, action: () => void, icon: React.ReactElement) => string;
            };
            sidebarMenu.registerSidebarBrowseOrAddChannelMenuAction?.(
                'Create a task',
                () => openNewTask(),
                <i className='icon icon-check-circle-outline'/>,
            );

            // The server publishes "custom_<plugin id>_task_changed" to channel members.
            registry.registerWebSocketEventHandler(`custom_${manifest.id}_task_changed`, () => notifyTasksChanged());
        } catch (err) {
            // eslint-disable-next-line no-console
            console.error(`[${manifest.id}] failed to initialize`, err);
        }

        // Registered on its own so a problem with the full-page board cannot take the panel down with it.
        try {
            registry.registerProduct({
                baseURL: BOARD_PATH,
                switcherIcon: 'product-checklists',
                switcherText: 'Tasks',
                switcherLinkURL: BOARD_PATH,
                mainComponent: BoardPage,
                headerCentreComponent: BoardHeader,
                showTeamSidebar: false,
            });
        } catch (err) {
            // eslint-disable-next-line no-console
            console.error(`[${manifest.id}] failed to register the board`, err);
        }
    }
}

declare global {
    interface Window {
        registerPlugin(pluginId: string, plugin: Plugin): void;
    }
}

window.registerPlugin(manifest.id, new Plugin());
