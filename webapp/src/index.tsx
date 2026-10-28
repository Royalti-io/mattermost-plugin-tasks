// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import manifest from 'manifest';
import React from 'react';
import type {Store} from 'redux';

import type {GlobalState} from '@mattermost/types/store';

import type {PluginRegistry} from 'types/mattermost-webapp';

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

const TasksPanel = () => (
    <div style={{padding: '16px'}}>
        <h3>{'Tasks'}</h3>
        <p>{'Development build. Nothing here yet.'}</p>
    </div>
);

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

export default class Plugin {
    public async initialize(registry: PluginRegistry, _store: Store<GlobalState>) {
        try {
            if (!(await uiEnabled())) {
                return;
            }

            const {toggleRHSPlugin} = registry.registerRightHandSidebarComponent(TasksPanel, 'Tasks');
            registry.registerChannelHeaderButtonAction(
                <TasksIcon/>,
                () => _store.dispatch(toggleRHSPlugin as never),
                'Tasks',
                'Tasks',
            );
        } catch (err) {
            // eslint-disable-next-line no-console
            console.error(`[${manifest.id}] failed to initialize`, err);
        }
    }
}

declare global {
    interface Window {
        registerPlugin(pluginId: string, plugin: Plugin): void;
    }
}

window.registerPlugin(manifest.id, new Plugin());
