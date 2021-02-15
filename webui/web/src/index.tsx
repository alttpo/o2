import 'preact/debug';
import {Fragment, render} from "preact";
import {StateUpdater, useState} from "preact/hooks";

import {GameViewModel, ROMViewModel, ServerViewModel, SNESViewModel, ViewModel} from './viewmodel';
import SNESView from "./snesview";
import ROMView from "./romview";

interface ViewModelUpdate {
    v: string;
    m: any;
}

export class CommandHandler {
    private ws: WebSocket;

    constructor(ws: WebSocket) {
        this.ws = ws;
    }

    command(view: string, command: string, args: object) {
        console.log(`command: ${view}.${command} args=${args}`);
        this.ws.send(JSON.stringify({
            v: view,
            c: command,
            a: args
        }));
    }

    binaryCommand(view: string, command: string, data: ArrayBuffer) {
        const te = new TextEncoder();
        const dataArr = new Uint8Array(data);

        // encode view and command names as Pascal strings and append `data`:
        const buf = new Uint8Array(view.length+1 + command.length+1 + dataArr.length);
        let i = 0;
        buf[i++] = view.length;
        i += te.encodeInto(view, buf.subarray(i)).written;

        buf[i++] = command.length;
        i += te.encodeInto(command, buf.subarray(i)).written;

        buf.set(dataArr, i);

        this.ws.send(buf);
    }
}

export class TopLevelProps {
    ch: CommandHandler;
    vm: ViewModel;
}

const App = () => {
    const [ws, setWs] = useState<WebSocket>(null);
    const [ch, setCh] = useState<CommandHandler>(null);

    const viewModelState: { [k: string]: [any, StateUpdater<any>] } = {
        status: useState<string>(""),
        snes: useState<SNESViewModel>({}),
        server: useState<ServerViewModel>({}),
        rom: useState<ROMViewModel>({}),
        game: useState<GameViewModel>({})
    };

    const viewModel = {
        status: viewModelState.status[0],
        snes: viewModelState.snes[0],
        server: viewModelState.server[0],
        rom: viewModelState.rom[0],
        game: viewModelState.game[0],
    };

    const connect = () => {
        const {protocol, host} = window.location;
        const url = (protocol === "https:" ? "wss:" : "ws:") + "//" + host + "/ws/";

        console.log("connect");
        const ws = new WebSocket(url);
        ws.onmessage = (e: MessageEvent<string>) => {
            let msg = JSON.parse(e.data) as ViewModelUpdate;
            let element = viewModelState[msg.v];
            element[1](msg.m);
        };
        setWs(ws);
        setCh(new CommandHandler(ws));
    };

    if (ws === null) {
        connect();
    }

    return (
        <Fragment>
            <header>
                <section class="rounded darken padded squeeze">
                    <h1>ALTTPO2</h1>
                </section>
            </header>
            <section class="squeeze">
                <div class="tabbed">
                    <div class="tab">
                        <input name="viewtab" id="viewtab1" type="radio" checked/>
                        <label for="viewtab1">SNES</label>
                        <div class="content">
                            <SNESView ch={ch} vm={viewModel}/>
                        </div>
                    </div>

                    <div class="tab">
                        <input name="viewtab" id="viewtab2" type="radio"/>
                        <label for="viewtab2">ROM</label>
                        <div class="content">
                            <ROMView ch={ch} vm={viewModel}/>
                        </div>
                    </div>

                    <div class="tab">
                        <input name="viewtab" id="viewtab3" type="radio"/>
                        <label for="viewtab3">Server</label>
                        <div v-view="server" class="content"></div>
                    </div>

                    <div class="tab">
                        <input name="viewtab" id="viewtab4" type="radio"/>
                        <label for="viewtab4">Game</label>
                        <div v-view="game" class="content"></div>
                    </div>
                </div>
            </section>
            <footer>
                <section class="rounded darken padded-lr squeeze">
                    <span>{viewModel.status}</span>
                </section>
            </footer>
        </Fragment>
    );
}

document.addEventListener("DOMContentLoaded", ev => {
        console.log("DOMContentLoaded");
        render(<App/>, document.querySelector('#app'));
    }
);
