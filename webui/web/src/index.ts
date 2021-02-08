import {ViewModel} from './viewmodel';

type ViewModelUpdate = {
    v: string;
    m: object;
}

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
        let msg = JSON.parse(e.data) as ViewModelUpdate;
        this.state.viewModel[msg.v] = msg.m;
    }
}

document.addEventListener("DOMContentLoaded", ev => {
    console.log("DOMContentLoaded");
    let state = new State();
    let host = new Host(state);


});
