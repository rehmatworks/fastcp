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
                            <div class="responsive-table">
                                <table class="table table-borderless table-striped">
                                    <tbody>
                                        <tr>
                                            <td style="width: 40%">SSH/SFTP User</td>
                                            <td>
                                                <span>{{ website.metadata.user }}</span>
                                                <small>
                                                    <a href="" class="text-danger">
                                                        <i class="fas fa-redo"></i> Reset
                                                        Password
                                                    </a>
                                                </small>
                                            </td>
                                        </tr>
                                        <tr>
                                            <td>SFTP/SSH Address</td>
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
                                                <i class="fab fa-php"></i> PHP
                                                {{ website.php }}
                                                <a href="" class="text-danger">Change</a>
                                            </td>
                                        </tr>
                                    </tbody>
                                </table>
                            </div>
                        </div>
                    </div>
                    <div class="card">
                        <div class="card-header text-light bg-primary">
                            Domains ({{ website.domains.length }})
                        </div>
                        <div class="card-body">
                            <div class="table-responsive">
                                <table class="table table-borderless table-striped">
                                    <tbody>
                                        <tr v-for="domain in website.domains" :key="domain.domain">
                                            <td style="width:40%;">{{ domain }}</td>
                                            <td class="text-success" style="width:30%;">
                                                <i class="fas fa-unlock"></i> HTTPS
                                            </td>
                                            <td class="text-right">
                                                <button class="btn btn-sm btn-danger">
                                                    Delete
                                                </button>
                                            </td>
                                        </tr>
                                    </tbody>
                                </table>
                            </div>
                        </div>
                    </div>
                    <div class="card mt-3">
                        <div :class="{'bg-secondary': !del, 'bg-danger': del}" class="card-header text-light">
                            Danger Zone
                        </div>
                        <div class="card-body">
                            Do you want to delete this website? You can do so here. Beware that this action is irreversible and you cannot undo once the website is deleted. Entire data associated to this website will be permanently deleted.
                        </div>
                        <div class="card-footer">
                            <button v-if="!del" @click="del=true" class="btn btn-secondary">
                                Delete Website
                            </button>
                            <button v-if="del" @click="del=false" class="btn btn-success">
                                Don't Delete
                            </button>
                            <button v-if="del" @click="deleteWebsite()" class="btn btn-danger">
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
            del: false
        };
    },
    created() {
        this.getWebsite();
    },
    methods: {
        getWebsite() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios
                .get(`/websites/${_this.$route.params.id}/`)
                .then((res) => {
                    _this.$store.commit("setBusy", false);
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
            axios.delete(`/websites/${_this.$route.params.id}/`).then((res) => {
                toastr.error('The website and associated data has been deleted.');
                _this.$router.push({name: 'websites'});
            }).catch((err) => {
                _this.$store.commit('setBusy', false);
                toastr.error('An error occured while trying to delete this website.');
            });
        }
    },
};
</script>
