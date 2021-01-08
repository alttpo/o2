

const App = {
    name: 'App',
    data: function() {
        return {
            ws: null
        };
    },
    methods: {

    },
    mounted() {
        console.log("mounted");
    },
    created: function() {
        const { protocol, host, pathname } = window.location;
        const url = (protocol === "https:" ? "wss:" : "ws:") + "//" + host + "/ws/";
        this.ws = new WebSocket(url);
        this.ws.onmessage = function (e) {
            console.log(e.data);
        };
    }
};

const vm = Vue.createApp(App);
vm.mount('#app');
