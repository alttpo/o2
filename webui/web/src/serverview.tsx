import {ServerViewModel} from "./viewmodel";
import {CommandHandler, TopLevelProps} from "./index";
import {Component} from 'preact';
import {StateUpdater, useState} from "preact/hooks";

type ServerProps = {
    ch: CommandHandler;
    server: ServerViewModel;
};

interface ServerState {
    hostName:   string;
    groupName:  string;
    teamNumber: string;
    playerName: string;
}

class ServerView extends Component<ServerProps> {
    constructor() {
        super();
    }

    render({ch, server}: ServerProps) {
        const state: ServerState = {
            hostName:   server.hostName,
            groupName:  server.groupName,
            teamNumber: server.teamNumber.toString(),
            playerName: server.playerName,
        };

        const cmdConnect = (e: Event) => {
            e.preventDefault();
            ch.command('server', 'connect', {});
        };
        const cmdDisconnect = (e: Event) => {
            e.preventDefault();
            ch.command('server', 'disconnect', {});
        };
        const cmdUpdate = () => {
            ch.command('server', 'update', {
                hostName:   state.hostName,
                groupName:  state.groupName,
                teamNumber: parseInt(state.teamNumber, 10),
                playerName: state.playerName,
            });
        };
        const onChanged = (key: keyof ServerState, e: Event) => {
            state[key] = ((e.currentTarget as HTMLInputElement).value);
            cmdUpdate();
        };

        const connectButton = () => {
            if (server.isConnected) {
                return <button type="button"
                               onClick={cmdDisconnect.bind(this)}>Disconnect</button>;
            } else {
                return <button type="button"
                               onClick={cmdConnect.bind(this)}>Connect</button>;
            }
        };

        return <div class="card">
            <label for="hostName">Hostname:</label>
            <input type="text" value={server.hostName} id="hostName" onChange={onChanged.bind(this, 'hostName')}/>
            <label for="groupName">Group:</label>
            <input type="text" value={server.groupName} id="groupName" onChange={onChanged.bind(this, 'groupName')}/>
            <label for="teamNumber">Team:</label>
            <input type="number" value={server.teamNumber} id="teamNumber" onChange={onChanged.bind(this, 'teamNumber')}/>
            <label for="playerName">Player:</label>
            <input type="text" value={server.playerName} id="playerName" onChange={onChanged.bind(this, 'playerName')}/>
            {connectButton()}
        </div>;
    }
}

export default ({ch, vm}: TopLevelProps) => (<ServerView ch={ch} server={vm.server}/>);
