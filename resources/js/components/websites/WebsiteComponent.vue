<template>
    <div class="row">
        <div class="col-12">
            <div class="row mb-2">
                <div class="col-12">
                    <h4>Manage Website: #{{ $route.params.id }}</h4>
                </div>
            </div>
            <div v-if="website" class="row">
                <div class="col-md-8">
                    <div class="card mb-3">
                        <div class="card-header bg-primary text-light">
                            General Details
                        </div>
                        <div class="card-body">
                            <p v-if="new_password">New password is <small class="text-info font-weight-bold">{{ new_password }}</small> and it is shown to you this single time. <a @click="new_password=false" class="text-danger text-decoration-none" style="font-size:14px;" href="javascript:void(0)"><i class="fas fa-times-circle"></i> Hide</a></p>
                            <div class="responsive-table">
                                <table class="table table-borderless table-striped">
                                    <tbody>
                                        <tr>
                                            <td style="width: 40%">SSH/SFTP User</td>
                                            <td>
                                                <span>{{ website.metadata.user }}</span>
                                                <small>
                                                    <a
                                                        v-if="!reset"
                                                        @click="reset = true"
                                                        href="javascript:void(0)"
                                                        class="text-danger"
                                                    >
                                                        <i class="fas fa-redo"></i> Reset
                                                        Password
                                                    </a>
                                                    <a
                                                        v-if="reset"
                                                        @click="reset = false"
                                                        href="javascript:void(0)"
                                                        class="text-success"
                                                    >
                                                        Cancel
                                                    </a>
                                                    <a
                                                        v-if="reset"
                                                        @click="resetPassword()"
                                                        href="javascript:void(0)"
                                                        class="text-danger"
                                                    >
                                                        Reset
                                                    </a>
                                                </small>
                                            </td>
                                        </tr>
                                        <tr>
                                            <td>SFTP/SSH Host</td>
                                            <td>
                                                {{ website.metadata.ip_addr }}
                                            </td>
                                        </tr>
                                        <tr>
                                            <td>Web Root</td>
                                            <td>
                                                {{ website.metadata.path }}
                                            </td>
                                        </tr>
                                        <tr>
                                            <td>PHP Version</td>
                                            <td>
                                                <a v-if="!change_php">
                                                    <i class="fab fa-php"></i> PHP
                                                    {{ website.php }}
                                                    <a @click="change_php=true" href="javascript:void(0)" class="text-danger"
                                                        >Change</a
                                                    >
                                                </a>
                                                <select v-else v-model="website.php">
                                                    <option v-for="php in php_versions" :key="php" :value="php">PHP {{ php }}</option>
                                                </select>
                                                <button v-if="change_php" @click="change_php=false" style="font-size:12px;" class="btn btn-primary btn-sm">Cancel</button>
                                                <button v-if="change_php" @click="changePhp()" style="font-size:12px;" class="btn btn-danger btn-sm">Update</button>
                                            </td>
                                        </tr>
                                    </tbody>
                                </table>
                            </div>
                        </div>
                    </div>
                    <div v-if="add_dom" class="card mb-3">
                        <div class="card-body">
                            <div class="form-group">
                                <label for="domain">Domain Name</label>
                                <input v-model="new_domain" :class="{'is-invalid': errors.domain}" type="text" class="form-control" placeholder="Add a new domain to this website.">
                                <small class="d-block invalid-feedback" v-if="errors.domain">
                                    {{ errors.domain[0] }}
                                </small>
                            </div>
                        </div>
                        <div class="card-footer">
                            <button @click="addDomain()" class="btn btn-primary">
                                Add Domain
                            </button>
                            <button @click="add_dom=false" class="btn btn-success">Cancel</button>
                        </div>
                    </div>
                    <div v-if="refresh_ssl" class="card mb-3">
                        <div class="card-header">
                            Refresh SSL Certificates
                        </div>
                        <div class="card-body">
                            <p>FastCP automatically obtains SSL certificates and automatically renews them for domains that point to this server. When a new domain is added, it may take up to 1 hour before an SSL certificate is obtained.</p>
                            <p>But if you are in a hurry and need to activate SSL certificates right away, you can request a refresh below. If SSL can't be activated, ensure that the domains are resolving to this server's IP.</p>
                        </div>
                        <div class="card-footer">
                            <button @click="refreshSslCerts()" class="btn btn-warning">
                                Refresh SSL
                            </button>
                            <button class="btn btn-primary" @click="refresh_ssl=false">
                                Cancel
                            </button>
                        </div>
                    </div>
                    <div class="card">
                        <div class="card-header text-light bg-primary">
                            Domains ({{ website.domains.length }})
                            <button @click="add_dom=!add_dom" class="btn btn-primary float-right">
                                <i class="fas fa-plus"></i> Add
                            </button>
                            <button @click="refresh_ssl=!refresh_ssl" class="btn btn-primary float-right">
                                <i class="fas fa-lock"></i> Refresh SSL
                            </button>
                        </div>
                        <div class="card-body">
                            <div class="table-responsive">
                                <table class="table table-borderless table-striped">
                                    <tbody>
                                        <tr
                                            v-for="domain in website.domains"
                                            :key="domain.domain"
                                        >
                                            <td style="width: 40%">{{ domain.domain }}</td>
                                            <td class="font-weight-bold" :class="{'text-success': domain.ssl, 'text-muted': !domain.ssl}" style="width: 20%">
                                                <span v-if="domain.ssl">
                                                    <i class="fas fa-lock"></i> HTTPS
                                                </span>
                                                <span v-else>
                                                    <i class="fas fa-unlock"></i> HTTP
                                                </span>
                                            </td>
                                            <td class="text-right">
                                                <button v-if="del_dom!=domain.id" @click="del_dom=domain.id" class="btn btn-sm btn-warning">
                                                    Delete
                                                </button>
                                                <button v-if="del_dom==domain.id" @click="deleteDomain(domain.id)" class="btn btn-sm btn-danger">
                                                    Confirm
                                                </button>
                                                <button v-if="del_dom==domain.id" @click="del_dom=false" class="btn btn-sm btn-success">
                                                    Cancel
                                                </button>
                                            </td>
                                        </tr>
                                    </tbody>
                                </table>
                            </div>
                        </div>
                    </div>
                    <div class="card mt-3">
                        <div
                            :class="{ 'bg-secondary': !del, 'bg-danger': del }"
                            class="card-header text-light"
                        >
                            Danger Zone
                        </div>
                        <div class="card-body">
                            Do you want to delete this website? You can do so here. Beware
                            that this action is irreversible and you cannot undo once the
                            website is deleted. Entire data associated to this website
                            will be permanently lost.
                        </div>
                        <div class="card-footer">
                            <button
                                v-if="!del"
                                @click="del = true"
                                class="btn btn-secondary"
                            >
                                Delete Website
                            </button>
                            <button
                                v-if="del"
                                @click="del = false"
                                class="btn btn-success"
                            >
                                Don't Delete
                            </button>
                            <button
                                v-if="del"
                                @click="deleteWebsite()"
                                class="btn btn-danger"
                            >
                                Confirm Deletion
                            </button>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    </div>
</template>
<script>
export default {
    data() {
        return {
            website: false,
            del: false,
            reset: false,
            change_php: false,
            php_versions: [],
            del_dom: false,
            new_domain: '',
            add_dom: false,
            refresh_ssl: false,
            new_password: false,
            errors: {}
        };
    },
    created() {
        this.getWebsite();
        this.getPhpVersions();
    },
    methods: {
        addDomain() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            _this.errors = {};
            let fd = new FormData();
            fd.append('domain', _this.new_domain);
            axios.post(`/websites/${_this.$route.params.id}/add-domain/`, fd).then((res) => {
                _this.add_dom = false;
                _this.new_domain = '';
                _this.$store.commit('setbusy', false);
                toastr.success('Domain has been added successfully.');
                _this.getWebsite();
            }).catch((err) => {
                if(err.response && err.response.data.errors) {
                    _this.errors = err.response.data.errors;
                } else {
                    toastr.error('An error occurred and the domain cannot be added.');
                }
                _this.$store.commit('setBusy', false);
            });
        },
        getPhpVersions() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios.get('/websites/php-versions/').then((res) => {
                _this.php_versions = res.data.php_versions;
                _this.$store.commit('setBusy', false);
            }).catch((err) => {
                toastr.error('Supported PHP versions list cannot be retrieved.');
                _this.$store.commit('setBusy', false);
            });
        },
        changePhp() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            let fd = new FormData();
            fd.append("php", _this.website.php);
            axios
                .post(`/websites/${_this.$route.params.id}/change-php/`, fd)
                .then((res) => {
                    _this.$store.commit('setBusy', false);
                    toastr.success('PHP version has been updated.');
                    _this.change_php = false;
                })
                .catch((err) => {
                    _this.$store.commit('setBusy', false);
                    toastr.error('PHP version cannot be updated.');
                });
        },
        resetPassword() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios
                .post(`/ssh-users/${_this.website.user}/reset-password/`)
                .then((res) => {
                    _this.$store.commit('setBusy', false);
                    toastr.success('SSH/SFTP password has been updated.');
                    _this.new_password = res.data.new_password;
                    _this.reset = false;
                })
                .catch((err) => {
                    _this.$store.commit('setBusy', false);
                    toastr.error('SSH/SFTP password cannot be updated.');
                });
        },
        getWebsite() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios
                .get(`/websites/${_this.$route.params.id}/`)
                .then((res) => {
                    _this.$store.commit('setBusy', false);
                    _this.website = res.data;
                })
                .catch((err) => {
                    _this.$store.commit('setBusy', false);
                    toastr.error('Cannot fetch the website data.');
                });
        },
        deleteWebsite() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios
                .delete(`/websites/${_this.$route.params.id}/`)
                .then((res) => {
                    toastr.error('The website and associated data has been deleted.');
                    _this.$router.push({ name: 'websites' });
                })
                .catch((err) => {
                    _this.$store.commit('setBusy', false);
                    toastr.error('An error occured while trying to delete this website.');
                });
        },
        deleteDomain(dom_id) {
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios.delete(`/websites/${_this.$route.params.id}/delete-domain/${dom_id}/`).then((res) => {
                _this.$store.commit('setBusy', false);
                _this.getWebsite();
                toastr.success('Domain has been deleted.');
            }).catch((err) => {
                _this.$store.commit('setBusy', false);
                if(err.response && err.response.data.message) {
                    toastr.error(err.response.data.message);
                } else {
                    toastr.error('Domain cannot be deleted.');
                }
            });
        },
        refreshSslCerts() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios.post(`/websites/${_this.$route.params.id}/refresh-ssl/`).then((res) => {
                _this.$store.commit('setBusy', false);
                _this.getWebsite();
                toastr.success(res.data.message);
                _this.refresh_ssl = false;
            }).catch((err) => {
                _this.$store.commit('setBusy', false);
                if(err.response && err.response.data.message) {
                    toastr.error(err.response.data.message);
                } else {
                    toastr.error('SSL certificates cannot be refreshed.');
                }
            });
        }
    },
};
</script>
