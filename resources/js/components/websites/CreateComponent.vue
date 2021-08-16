<template>
    <div class="row">
        <div class="col-12">
            <div class="card">
                <div class="card-header">
                    <h4 class="float-left mt-1">Deploy Website</h4>
                    <router-link
                        :to="{ name: 'websites' }"
                        class="btn mt-1 btn-sm float-right btn-warning"
                    >
                        <i class="fas fa-times"></i> Cancel
                    </router-link>
                </div>
                <div class="card-body">
                    <div class="row">
                        <div class="col-md-6">
                            <div class="form-group">
                                <label for="label">Website Label</label>
                                <input
                                    id="label"
                                    type="text"
                                    class="form-control"
                                    v-model="label"
                                    placeholder="A unique label to identify your website..."
                                />
                            </div>
                            <div class="form-group">
                                <label for="php">PHP Version</label>
                                <select id="php" v-model="php" class="form-control">
                                    <option v-for="php in php_versions" :key="php" :value="php">PHP {{ php }}</option>
                                </select>
                            </div>
                            <div class="form-group">
                                <label for="domains">Domains</label>
                                <input
                                    id="domains"
                                    type="text"
                                    class="form-control"
                                    v-model="domains"
                                    placeholder="Comma-separated domains list, i.e. example.com, www.example.com"
                                />
                            </div>
                        </div>
                    </div>
                </div>
                <div class="card-footer">
                    <button class="btn btn-primary">Deploy Now</button>
                </div>
            </div>
        </div>
    </div>
</template>
<script>
export default {
    data() {
        return {
            label: '',
            php: '',
            php_versions: [],
            domains: ''
        }
    },
    created() {
        this.getPhpVersions();
    },
    methods: {
        getPhpVersions() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios.get('/websites/php-versions/').then((res) => {
                _this.php_versions = res.data.php_versions;
                _this.php = _this.php_versions[0];
                _this.$store.commit('setBusy', false);
            }).catch((err) => {
                toastr.error('Supported PHP versions list cannot be retrieved.');
                _this.$store.commit('setBusy', false);
            });
        }
    }
};
</script>
