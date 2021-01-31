import {ViewModel} from './viewmodel';
import {O2IncomingMessage} from './messages';

class State {
    public viewModel: ViewModel;
}

class Host {
    private state: State;
    private ws: WebSocket;

    constructor(state: State) {
        this.state = state;

        const {protocol, host} = window.location;
        const url = (protocol === "https:" ? "wss:" : "ws:") + "//" + host + "/ws/";

        this.ws = new WebSocket(url);
        this.ws.onmessage = this.onmessage;
    }

    onmessage(e: MessageEvent<string>) {
        let msg = JSON.parse(e.data) as O2IncomingMessage;
        switch (msg.c) {
            case "vmu": // view-model update
                this.state.viewModel = msg.d;
                break;
        }
    }
}

document.addEventListener("load", ev => {
    let state = new State();
    let host = new Host(state);
});
