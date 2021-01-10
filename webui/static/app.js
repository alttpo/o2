
let host;
class Host {
    constructor() {
        const { protocol, host } = window.location;
        const url = (protocol === "https:" ? "wss:" : "ws:") + "//" + host + "/ws/";
        this.ws = new WebSocket(url);
        this.ws.onmessage = this.onmessage;
    }

    onmessage(e) {
        let msg = JSON.parse(e.data);
        switch (msg.c) {
        case "devices":
            vm.provide()
            break;
        }
    }
}

const App = {
    name: 'App',
    data: function() {
        return {
            host: null
        };
    },
    methods: {
    },
    mounted() {
        console.log("mounted");
    },
    created: function() {
        host = new Host();
    }
};

const vm = Vue.createApp(App);
vm.mount('#app');
