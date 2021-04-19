import 'preact/debug';
import {Component, createRef, Fragment, h, render} from "preact";
import {StateUpdater, useState} from "preact/hooks";

// @ts-ignore
import ReactHintFactory from 'react-hint'
const ReactHint = ReactHintFactory({Component, createElement: h, createRef: createRef})
// NOTE: copied into r/css.css
//import 'react-hint/css/index.css'

import {GameViewModel, ROMViewModel, ServerViewModel, SNESViewModel, ViewModel} from './viewmodel';
import SNESView from "./snesview";
import ROMView from "./romview";
import ServerView from "./serverview";
import GameView from "./gameview";
import {JSXInternal} from "preact/src/jsx";
import TargetedEvent = JSXInternal.TargetedEvent;

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
        console.log(`json command: ${view}.${command}`);
        this.ws.send(JSON.stringify({
            v: view,
            c: command,
            a: args
        }));
    }

    binaryCommand(view: string, command: string, data: ArrayBuffer) {
        console.log(`binary command: ${view}.${command}`);

        const te = new TextEncoder();
        const dataArr = new Uint8Array(data);

        // encode view and command names as Pascal strings and append `data`:
        const buf = new Uint8Array(view.length + 1 + command.length + 1 + dataArr.length);
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
    type TabName = "snes" | "rom" | "server" | "game";

    const [ws, setWs] = useState<WebSocket>(null);
    const [ch, setCh] = useState<CommandHandler>(null);
    const [tabSelected, setTabSelected] = useState<TabName>("snes");

    const viewModelState: { [k: string]: [any, StateUpdater<any>] } = {
        status: useState<string>(""),
        snes: useState<SNESViewModel>({
            drivers: [], isConnected: false
        }),
        rom: useState<ROMViewModel>({
            isLoaded: false, region: "", name: "", title: "", version: ""
        }),
        server: useState<ServerViewModel>({
            isConnected: false, hostName: "", groupName: "", playerName: "", team: 0
        }),
        game: useState<GameViewModel>({
            isCreated: false,
            gameName: ""
        })
    };

    const viewModel = {
        status: viewModelState.status[0],
        snes: viewModelState.snes[0],
        server: viewModelState.server[0],
        rom: viewModelState.rom[0],
        game: viewModelState.game[0],
    };

    const tabChanged = (e: TargetedEvent<HTMLInputElement, Event>) => {
        setTabSelected(e.currentTarget.value as TabName);
    };

    const connect = () => {
        const {protocol, host} = window.location;
        const url = `${protocol === "https:" ? "wss:" : "ws:"}//${host}/ws/`;

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
                    <h1>O2{
                        (viewModel.game.isCreated) ? " - " + viewModel.game.gameName : ""
                    }</h1>
                </section>
            </header>
            <section class="squeeze">
                <div class="flex-wrap">
                    <div class="content flex-1">
                        <SNESView ch={ch} vm={viewModel}/>
                    </div>

                    <div class="content flex-1">
                        <ROMView ch={ch} vm={viewModel}/>
                    </div>

                    <div class="content flex-1">
                        <ServerView ch={ch} vm={viewModel}/>
                    </div>

                    {viewModel.game.isCreated && (
                        <div class="content flex-1">
                            <GameView ch={ch} vm={viewModel}/>
                        </div>
                    )}
                </div>
            </section>
            <footer>
                <section class="rounded darken padded-lr squeeze">
                    <span>{viewModel.status}</span>
                    <a style="float:right" href="/log.txt">Download Logs</a>
                </section>
            </footer>
            <ReactHint autoPosition events />
        </Fragment>
    );
}

document.addEventListener(
    "DOMContentLoaded",
    ev => {
        console.log("DOMContentLoaded");
        render(<App/>, document.querySelector('#app'));
    }
);
