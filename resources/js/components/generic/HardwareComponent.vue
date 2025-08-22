<template>
    <div v-if="info" class="row">
        <div class="col-12">
            <h4 class="float-left">Hardware Details</h4>
        </div>
        <div class="col-md-12">
            <div class="table-responsive">
                <table class="table table-striped table-bordered">
                    <tbody>
                        <tr>
                            <td style="width:40%;">CPU Cores</td>
                            <td>{{ info.cpu.physical }} physical <span class="badge badge-secondary">{{ info.cpu.logical }} virtual</span></td>
                        </tr>
                        <tr>
                            <td>CPU Load</td>
                            <td>{{ ((((info.cpu.load[0] + info.cpu.load[1] + info.cpu.load[2]) / 3) / info.cpu.logical) * 100) | floatformat }}%</td>
                        </tr>
                        <tr>
                            <td>RAM</td>
                            <td>{{ info.ram.memory.total | prettyBytes }} <span class="badge badge-secondary">{{ info.ram.memory.percent|floatformat }}% used</span></td>
                        </tr>
                        <tr>
                            <td>Swap</td>
                            <td>{{ info.ram.swap.total | prettyBytes }} <span class="badge badge-secondary">{{ info.ram.swap.percent|floatformat }}% used</span></td>
                        </tr>
                        <tr>
                            <td>Storage</td>
                            <td>{{ info.disk.total | prettyBytes }} <span class="badge badge-secondary">{{ info.disk.percent|floatformat }}% used</span></td>
                        </tr>
                        <tr>
                            <td>Uptime</td>
                            <td>
                                <span class="badge badge-secondary">{{ info.uptime }}</span>
                            </td>
                        </tr>
                    </tbody>
                </table>
            </div>
        </div>
    </div>
</template>
<script>
export default {
    data() {
        return {
            info: false
        }
    },
    mounted() {
        this.getHardwareInfo();
    },
    methods: {
        getHardwareInfo() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios.get('/stats/hardware/').then((res) => {
                _this.$store.commit('setBusy', false);
                _this.info = res.data;
            }).catch((err) => {
                _this.$store.commit('setBusy', false);
            });
        }
    }
}
</script>