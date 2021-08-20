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
                                    :class="{'is-invalid': errors.label}"
                                    v-model="label"
                                    placeholder="A unique label to identify your website..."
                                />
                                <p class="invalid-feedback" v-if="errors.label">{{ errors.label[0] }}</p>
                            </div>
                            <div class="form-group">
                                <label for="php">PHP Version</label>
                                <select :class="{'is-invalid': errors.php}" id="php" v-model="php" class="form-control">
                                    <option v-for="php in php_versions" :key="php" :value="php">PHP {{ php }}</option>
                                </select>
                                <p class="invalid-feedback" v-if="errors.php">{{ errors.php[0] }}</p>
                            </div>
                            <div class="form-group">
                                <label for="domains">Domains</label>
                                <input
                                    id="domains"
                                    type="text"
                                    class="form-control"
                                    :class="{'is-invalid': errors.domains}"
                                    v-model="domains"
                                    placeholder="Comma-separated domains list, i.e. example.com, www.example.com"
                                />
                                <p class="invalid-feedback" v-if="errors.domains">{{ errors.domains[0] }}</p>
                            </div>
                        </div>
                    </div>
                </div>
                <div class="card-footer">
                    <button @click="createWebsite()" class="btn btn-primary">Deploy Now</button>
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
            domains: '',
            errors: {}
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
        },
        createWebsite() {
            let _this = this;
            _this.errors = {};
            _this.$store.commit('setBusy', true);
            let fd = new FormData();
            fd.append('label', _this.label);
            fd.append('php', _this.php);
            fd.append('domains', _this.domains);
            axios.post('/websites/', fd).then((res) => {
                _this.$store.commit('setBusy', false);
                toastr.success('Website has been added successfully.');
                _this.$router.push({name: 'websites'});
            }).catch((err) => {
                _this.$store.commit('setBusy', false);
                _this.errors = err.response.data;
            });
        }
    }
};
</script>
