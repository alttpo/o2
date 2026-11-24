import {ServerViewModel} from "./viewmodel";
import {CommandHandler, TopLevelProps} from "./index";
import {useEffect, useState} from "preact/hooks";
import {Fragment} from "preact";
import {setField} from "./util";

type ServerProps = {
    ch: CommandHandler;
    server: ServerViewModel;
};

function ServerView({ch, server}: ServerProps) {
    const [collapsed, set_collapsed] = useState(false);

    const [hostName, setHostName] = useState('');
    const [groupName, setGroupName] = useState('');
    const [password, setPassword] = useState('');
    const [playerName, setPlayerName] = useState('');
    const [team, setTeam] = useState(0);

    useEffect(() => {
        setHostName(server?.hostName);
        setGroupName(server?.groupName);
        setPassword(server?.password ?? '');
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

    // two buttons like alttpo's, the one that does not apply grayed out:
    const connectButtons = () => <Fragment>
        <span/>
        <button type="button"
                disabled={!!server?.isConnected}
                title="Connect to the server and join your group"
                onClick={cmdConnect.bind(this)}>Connect</button>
        <span/>
        <button type="button"
                disabled={!server?.isConnected}
                title="Disconnect from the server"
                onClick={cmdDisconnect.bind(this)}>Disconnect</button>
    </Fragment>;

    const getTargetValueString = (e: Event) => (e.target as HTMLInputElement).value;
    const getTargetValueInt = (e: Event) => parseInt((e.target as HTMLInputElement).value, 10);
    return <div class={"grid collapsible" + (collapsed ? " collapsed" : "")} style="grid-template-columns: max-content minmax(0, 1fr)">
        <h5 style="grid-column: 1 / span 2">
            <span data-rh-at="left" data-rh="To play online with other players, connect to a server
and enter a group name you wish to join. Groups are created on the fly by whoever enters the group name first, who can also protect it with a password."
        >Connect to a server:&nbsp;2️⃣</span>
            <span class="collapse-icon" onClick={() => set_collapsed(st => !st)}>{ collapsed ? "🔽": "🔼" }</span>
        </h5>
        <label for="hostName">Hostname:</label>
        <input type="text"
               style="min-width: 0; width: 100%; box-sizing: border-box"
               value={hostName}
               disabled={server?.isConnected}
               title="Connect to an O2 server (default is `alttp.online`)"
               id="hostName"
               onInput={setField.bind(this, sendServerCommand, setHostName, "hostName", getTargetValueString)}/>
        <label for="groupName">Group:</label>
        <input type="text"
               style="min-width: 0; width: 100%; box-sizing: border-box"
               value={groupName}
               title="A group name uniquely identifies the group of players you wish to sync items and progress with; max 20 characters, case-insensitive, leading and trailing whitespace are trimmed"
               id="groupName"
               onInput={setField.bind(this, sendServerCommand, setGroupName, "groupName", getTargetValueString)}/>
        <label for="password">Password:</label>
        <input type="password"
               autocomplete="off"
               style="min-width: 0; width: 100%; box-sizing: border-box"
               value={password}
               title="Optional. If you are the first to join the group, this sets the group's password; everyone else must enter the same one. Only a hash of it is sent, so it keeps uninvited players out but does not hide from someone watching network traffic."
               id="password"
               onInput={setField.bind(this, sendServerCommand, setPassword, "password", getTargetValueString)}/>

        <label for="playerName">Player Name:</label>
        <input type="text"
               style="min-width: 0; width: 100%; box-sizing: border-box"
               value={playerName}
               title="Enter your player name here"
               id="playerName"
               onInput={setField.bind(this, sendServerCommand, setPlayerName, "playerName", getTargetValueString)}/>
        <label for="team">Team Number:</label>
        <input type="number"
               style="min-width: 0; width: 100%; box-sizing: border-box"
               min={0}
               max={255}
               value={team}
               title="The team number within the group you wish to sync with; default is 0 to sync with all players, max 255"
               id="team"
               onInput={setField.bind(this, sendServerCommand, setTeam, "team", getTargetValueInt)}/>

        {connectButtons()}
    </div>;
}

export default ({ch, vm}: TopLevelProps) => (<ServerView ch={ch} server={vm.server}/>);
