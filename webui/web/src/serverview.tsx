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
        setHostName(server?.hostName);
        setGroupName(server?.groupName);
        setPlayerName(server?.playerName);
        setTeam(server?.team);
    }, [server]);

    // NOTE: `ch` can be null during app init
    const sendServerCommand = ch?.command?.bind(ch, "server");

    const cmdConnect = (e: Event) => {
        e.preventDefault();
        sendServerCommand('connect', {});
    };

    const cmdDisconnect = (e: Event) => {
        e.preventDefault();
        sendServerCommand('disconnect', {});
    };

    const connectButton = () => {
        if (server?.isConnected) {
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
    return <div class="grid" style="min-width: 20em">
        <h5 class="grid-ca"><span data-rh-at="left" data-rh="To play online with other players, connect to a server
and enter a group name you wish to join. Groups are created on the fly by whoever enters the group name first."
        >Connect to a server:&nbsp;4️⃣</span></h5>
        <label class="grid-c1" for="hostName">Hostname:</label>
        <input type="text"
               value={hostName}
               disabled={server?.isConnected}
               title="Connect to an O2 server (default is `alttp.online`)"
               id="hostName"
               class="grid-c2"
               onInput={setField.bind(this, sendServerCommand, setHostName, "hostName", getTargetValueString)}/>
        <label class="grid-c1" for="groupName">Group:</label>
        <input type="text"
               value={groupName}
               title="A group name uniquely identifies the group of players you wish to sync items and progress with; max 20 characters, case-insensitive, leading and trailing whitespace are trimmed"
               id="groupName"
               class="grid-c2"
               onInput={setField.bind(this, sendServerCommand, setGroupName, "groupName", getTargetValueString)}/>

        <label class="grid-c1" for="playerName">Player Name:</label>
        <input type="text"
               value={playerName}
               title="Enter your player name here"
               id="playerName"
               class="grid-c2"
               onInput={setField.bind(this, sendServerCommand, setPlayerName, "playerName", getTargetValueString)}/>
        <label class="grid-c1" for="team">Team Number:</label>
        <input type="number"
               min={0}
               max={255}
               value={team}
               title="The team number within the group you wish to sync with; default is 0 to sync with all players, max 255"
               id="team"
               class="grid-c2"
               onInput={setField.bind(this, sendServerCommand, setTeam, "team", getTargetValueInt)}/>

        {connectButton()}
    </div>;
}

export default ({ch, vm}: TopLevelProps) => (<ServerView ch={ch} server={vm.server}/>);
