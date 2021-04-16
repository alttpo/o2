import {ServerViewModel} from "./viewmodel";
import {CommandHandler, TopLevelProps} from "./index";
import {useEffect, useState} from "preact/hooks";
import {setField} from "./util";

type ServerProps = {
    ch: CommandHandler;
    server: ServerViewModel;
};

function ServerView({ch, server}: ServerProps) {
    const [hostName, setHostName] = useState('');
    const [groupName, setGroupName] = useState('');
    const [playerName, setPlayerName] = useState('');
    const [team, setTeam] = useState(0);

    useEffect(() => {
        setHostName(server.hostName);
        setGroupName(server.groupName);
        setPlayerName(server.playerName);
        setTeam(server.team);
    }, [server]);

    const sendServerCommand = ch?.command?.bind(ch, "server");

    const cmdConnect = (e: Event) => {
        e.preventDefault();
        sendServerCommand('connect', {
            hostName,
            groupName
        });
    };

    const cmdDisconnect = (e: Event) => {
        e.preventDefault();
        sendServerCommand('disconnect', {});
    };

    const connectButton = () => {
        if (server.isConnected) {
            return <button type="button"
                           class="grid-c2"
                           onClick={cmdDisconnect.bind(this)}>Disconnect</button>;
        } else {
            return <button type="button"
                           class="grid-c2"
                           onClick={cmdConnect.bind(this)}>Connect</button>;
        }
    };

    const getTargetValueString = (e: Event) => (e.target as HTMLInputElement).value;
    const getTargetValueInt = (e: Event) => parseInt((e.target as HTMLInputElement).value, 10);
    return <div class="grid" style="min-width: 24em">
        <h5 class="grid-ca">Connect to a server:</h5>
        <label class="grid-c1" for="hostName">Hostname:</label>
        <input type="text"
               value={hostName}
               disabled={server.isConnected}
               title="Connect to a server (default is `alttp.online`)"
               id="hostName"
               class="grid-c2"
               onInput={e => setHostName((e.target as HTMLInputElement).value)}/>
        <label class="grid-c1" for="groupName">Group:</label>
        <input type="text"
               value={groupName}
               disabled={server.isConnected}
               id="groupName"
               class="grid-c2"
               onInput={e => setGroupName((e.target as HTMLInputElement).value)}/>

        <label class="grid-c1" for="playerName">Player Name:</label>
        <input type="text"
               value={playerName}
               id="playerName"
               class="grid-c2"
               onInput={setField.bind(this, sendServerCommand, setPlayerName, "playerName", getTargetValueString)}/>
        <label class="grid-c1" for="team">Team Number:</label>
        <input type="number"
               min={0}
               max={255}
               value={team}
               id="team"
               class="grid-c2"
               onInput={setField.bind(this, sendServerCommand, setTeam, "team", getTargetValueInt)}/>

        {connectButton()}
    </div>;
}

export default ({ch, vm}: TopLevelProps) => (<ServerView ch={ch} server={vm.server}/>);
