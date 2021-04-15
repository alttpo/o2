import {ServerViewModel} from "./viewmodel";
import {CommandHandler, TopLevelProps} from "./index";
import {useEffect, useState} from "preact/hooks";

type ServerProps = {
    ch: CommandHandler;
    server: ServerViewModel;
};

function ServerView({ch, server}: ServerProps) {
    const [hostName, setHostName] = useState('');
    const [groupName, setGroupName] = useState('');

    useEffect(() => {
        setHostName(server.hostName);
        setGroupName(server.groupName);
    }, [server]);

    const cmdConnect = (e: Event) => {
        e.preventDefault();
        ch.command('server', 'connect', {
            hostName,
            groupName
        });
    };
    const cmdDisconnect = (e: Event) => {
        e.preventDefault();
        ch.command('server', 'disconnect', {});
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

    return <div class="card three-grid">
        <label class="grid-col1" for="hostName">Hostname:</label>
        <input type="text" value={hostName} disabled={server.isConnected} id="hostName"
               onInput={e => setHostName((e.target as HTMLInputElement).value)}/>
        <label class="grid-col1" for="groupName">Group:</label>
        <input type="text" value={groupName} disabled={server.isConnected} id="groupName"
               onInput={e => setGroupName((e.target as HTMLInputElement).value)}/>
        {connectButton()}
    </div>;
}

export default ({ch, vm}: TopLevelProps) => (<ServerView ch={ch} server={vm.server}/>);
