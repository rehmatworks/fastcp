<template>
    <div v-if="files" class="row">
        <div class="col-12 mb-2">
            <div class="row">
                <div class="col-12">
                    <h4>File Manager ({{ files.count }})</h4>
                    <p v-if="files.segments.length" class="d-block">
                        Browsing: ~/apps<a v-for="segment in files.segments" v-if="segment[0] > 3" class="text-info" :key="segment[0]" @click="browseSegment(segment[0])" href="javascript:void(0)">/{{ segment[1] }}</a>
                    </p>
                    <p v-if="(move || copy) && move_selected.length > 0" class="border p-1 file-toolbar rounded">
                        You are going to <span v-if="move">move</span><span v-else>copy</span> {{ move_selected.length }} <span v-if="move_selected.length == 1">item</span><span v-else>items</span>. Now go to the destination directory and click confirm.
                        <button @click="moveItems()" :disabled="!validDestination()" class="btn btn-sm btn-danger">Confirm</button>
                        <button @click="move_selected=[];move=false;" class="btn btn-sm btn-primary">Cancel</button>
                    </p>
                    <p v-if="del" class="border p-1 file-toolbar rounded">
                        Do you really want to permanently delete the selected
                        {{ selected.length }} <span v-if="selected.length == 1">item</span
                        ><span v-else>items</span>?
                        <a
                            href="javascript:void(0)"
                            class="text-success mr-1"
                            @click="(del = false), (confirm = false)"
                            >Cancel</a
                        >
                        <a
                            href="javascript:void(0)"
                            class="text-danger"
                            @click="deleteItems()"
                            >Confirm</a
                        >
                    </p>
                    <p v-else-if="extract" class="border p-1 file-toolbar rounded">
                        Do you really want to extract the selected archive's content in
                        the current directory?
                        <a
                            href="javascript:void(0)"
                            class="text-success mr-1"
                            @click="(del = false), (confirm = false)"
                            >Cancel</a
                        >
                        <a
                            href="javascript:void(0)"
                            class="text-danger"
                            @click="extractArchive()"
                            >Confirm</a
                        >
                    </p>
                    <p v-else-if="create">
                        <input v-model="itemname" type="text" placeholder="Name" />
                        <select v-model="itemtype">
                            <option value="file">File</option>
                            <option value="directory">Directory</option>
                        </select>
                        <button
                            @click="createItem()"
                            class="btn btn-info btn-sm"
                            :disabled="!itemname"
                        >
                            Create
                        </button>
                        <button @click="create = false" class="btn btn-warning btn-sm">
                            Cancel
                        </button>
                    </p>
                    <p v-else-if="remote_upl">
                        <input style="max-width:inherit;width:80%;" v-model="remote_url" type="url" placeholder="Enter a remote public URL to download any file over your server network." />
                        <button
                            @click="fetchRemote()"
                            class="btn btn-info btn-sm"
                            :disabled="!remote_url"
                        >
                            Fetch
                        </button>
                        <button @click="remote_upl = false" class="btn btn-warning btn-sm">
                            Cancel
                        </button>
                        <small class="text-danger d-block mt-0" v-if="errors.remote_url">{{ errors.remote_url[0] }}</small>
                    </p>
                    <p class="border mb-1 mt-1 file-toolbar rounded">
                        <nav class="navbar navbar-expand-lg navbar-light bg-light">
                            <button class="navbar-toggler" type="button" data-toggle="collapse" data-target="#navbarNavAltMarkup" aria-controls="navbarNavAltMarkup" aria-expanded="false" aria-label="Toggle navigation">
                                <span class="navbar-toggler-icon"></span>
                            </button>
                            <div class="collapse navbar-collapse" id="navbarNavAltMarkup">
                                <div class="navbar-nav">
                                    <a
                                        href="javascript:void(0)"
                                        class="nav-item nav-link"
                                        :class="{ 'disabled': $store.state.path == web_root }"
                                        @click="goHome()"
                                    >
                                        <i class="fas fa-home"></i> Home
                                    </a>
                                    <a
                                        :class="{ 'disabled': $store.state.path == web_root }"
                                        href="javascript:void(0)"
                                        class="nav-item nav-link"
                                        @click="goBack()"
                                    >
                                        <i class="fas fa-arrow-left"></i> Back
                                    </a>
                                    <a
                                        @click="chooseFiles()"
                                        href="javascript:void(0)"
                                        class="nav-item nav-link"
                                    >
                                        <i class="fas fa-upload"></i> Upload
                                    </a>
                                    <a
                                        @click="remote_upl=!remote_upl"
                                        href="javascript:void(0)"
                                        class="nav-item nav-link"
                                    >
                                        <i class="fas fa-globe"></i> Remote Fetch
                                    </a>
                                    <a
                                        @click="create = true"
                                        href="javascript:void(0)"
                                        class="nav-item nav-link"
                                    >
                                        <i class="fas fa-plus"></i> Create
                                    </a>
                                    <a @click="getFiles()" href="javascript:void(0)" class="nav-item nav-link">
                                        <i class="fas fa-redo"></i> Refresh
                                    </a>
                                    <a
                                        @click="
                                            if (selected.length > 0) {
                                                move = true;
                                                copy = false;
                                                move_selected = selected;
                                                selall = false;
                                                selected = [];
                                            }
                                        "
                                        href="javascript:void(0)"
                                        :class="{ 'disabled': selected.length == 0 }"
                                        class="nav-item nav-link"
                                    >
                                        <i class="fas fa-arrows-alt"></i> Move
                                    </a>
                                    <a
                                        @click="
                                            if (selected.length > 0) {
                                                copy = true;
                                                move = false;
                                                move_selected = selected;
                                                selall = false;
                                                selected = [];
                                            }
                                        "
                                        href="javascript:void(0)"
                                        :class="{ 'disabled': selected.length == 0 }"
                                        class="nav-item nav-link"
                                    >
                                        <i class="fas fa-copy"></i> Copy
                                    </a>
                                    <a
                                        @click="prepareRename()"
                                        href="javascript:void(0)"
                                        :class="{ 'disabled': selected.length != 1 }"
                                        class="nav-item nav-link"
                                    >
                                        <i class="fas fa-retweet"></i> Rename
                                    </a>
                                    <a
                                        @click="compressFiles()"
                                        :class="{ 'disabled': selected.length == 0 }"
                                        href="javascript:void(0)"
                                        class="nav-item nav-link"
                                    >
                                        <i class="fas fa-archive"></i> Compress
                                    </a>
                                    <a
                                        @click="
                                            if (selected.length > 0) {
                                                extract = true;
                                            }
                                        "
                                        :class="{ 'disabled': !isZip(selected) }"
                                        href="javascript:void(0)"
                                        class="nav-item nav-link"
                                    >
                                        <i class="fas fa-file-archive"></i> Extract
                                    </a>
                                    <a
                                        @click="
                                            if (selected.length > 0) {
                                                del = true;
                                            }
                                        "
                                        :class="{ 'disabled': selected.length == 0 }"
                                        href="javascript:void(0)"
                                        class="nav-item nav-link"
                                    >
                                        <i class="fas fa-trash"></i> Delete
                                    </a>
                                </div>
                            </div>
                        </nav>
                    </p>
                </div>
            </div>
        </div>
        <div class="col-12">
            <div class="table-responsive">
                <table v-if="files.count > 0" class="table table-striped">
                    <thead class="bg-primary text-white">
                        <tr>
                            <th v-if="!move_selected.length" style="width: 2%">
                                <input v-model="selall" type="checkbox" />
                            </th>
                            <th style="width: 25%">File name</th>
                            <th style="width: 10%">Size</th>
                            <th style="width: 25%">Permissions</th>
                            <th colspan="2">Modified</th>
                        </tr>
                    </thead>
                    <tbody>
                        <tr v-for="file in files.results" :key="file.path">
                            <td v-if="!move_selected.length">
                                <input
                                    @click="selectItem(file.path)"
                                    :checked="selected.includes(file.path)"
                                    type="checkbox"
                                />
                            </td>
                            <td>
                                <span v-if="rename == file.path">
                                    <input type="text" v-model="new_name" />
                                    <button @click="renameItem()" class="btn btn-warning btn-sm">Save</button>
                                    <button class="btn btn-sm btn-primary" @click="rename=false,selected=[]">Cancel</button>
                                </span>
                                <span @click="browseFile(file)" v-else>
                                    <i
                                        v-if="file.file_type == 'directory'"
                                        class="fas text-warning fa-folder"
                                    ></i
                                    ><i v-else class="fas fa-file"></i> {{ file.name }}
                                </span>
                            </td>
                            <td>{{ file.size | prettyBytes }}</td>
                            <td>
                                <span v-if="edit_permissions==file.path">
                                    <input type="text" v-model="new_permissions" />
                                    <button @click="updatePermissions()" class="btn btn-warning btn-sm">Save</button>
                                    <button class="btn btn-sm btn-primary" @click="edit_permissions=false,new_permissions=''">Cancel</button>
                                </span>
                                <span @click="edit_permissions=file.path,new_permissions=file.permissions" v-else>
                                    {{ file.permissions }}
                                </span>
                            </td>
                            <td>{{ file.modified }}</td>
                            <td class="text-right">
                                <button
                                    v-if="file.file_type == 'file'"
                                    class="btn btn-sm btn-outline-info"
                                    @click="editFile(file)"
                                    data-toggle="modal"
                                    data-target="#fileEditModal"
                                >
                                    <i class="fas fa-edit"></i>
                                </button>
                                <a
                                    v-if="file.file_type == 'file'"
                                    class="btn btn-sm btn-outline-primary"
                                    :href="'/dashboard/download-file/?path=' + encodeURIComponent(file.path)"
                                    target="_blank"
                                    rel="nofollow noopener"
                                >
                                    <i class="fas fa-download"></i>
                                </a>
                            </td>
                        </tr>
                    </tbody>
                </table>
                <div
                    v-else
                    class="text-muted mb-3 border border-muted rounded p-5 text-center"
                >
                    No files or directories found here.
                    <a class="text-decoration-none" @click="create=true" href="javascript:void(0)">
                        Create 
                    </a> a file or a directory or
                    <a class="text-decoration-none" @click="chooseFiles()" href="javascript:void(0)">
                        upload 
                    </a> files.
                </div>
                <nav v-if="files && files.count > 0" aria-label="Pagination">
                    <ul class="pagination float-right">
                        <li class="page-item" @click="getFiles(files.links.previous)" :class="{'disabled': !files.links.previous }"><a class="page-link" href="javascript:void(0)">Previous</a></li>
                        <li class="page-item" @click="getFiles(files.links.next)" :class="{'disabled': !files.links.next }"><a class="page-link" href="javascript:void(0)">Next</a></li>
                    </ul>
                </nav>
            </div>
        </div>
        <div
            class="modal fade"
            id="fileEditModal"
            tabindex="-1"
            role="dialog"
            aria-labelledby="fileEditModal"
            aria-hidden="true"
        >
            <div class="modal-dialog modal-lg" role="document">
                <div class="modal-content">
                    <div class="modal-header">
                        <h5 class="modal-title" id="editLabel">Edit {{ edit.name }}</h5>
                        <button
                            type="button"
                            class="close"
                            data-dismiss="modal"
                            aria-label="Close"
                        >
                            <span aria-hidden="true">&times;</span>
                        </button>
                    </div>
                    <div class="modal-body">
                        <textarea
                            :disabled="bad_file"
                            class="form-control"
                            rows="20"
                            v-model="edit_content"
                        ></textarea>
                    </div>
                    <div class="modal-footer">
                        <button
                            type="button"
                            class="btn btn-secondary"
                            data-dismiss="modal"
                        >
                            Close
                        </button>
                        <button
                            @click="saveFile()"
                            :disabled="bad_file || saving"
                            type="button"
                            class="btn btn-primary"
                        >
                            Save Changes
                        </button>
                    </div>
                </div>
            </div>
        </div>
        <input
            id="fileUpload"
            ref="file"
            v-on:change="handleFileUpload()"
            type="file"
            hidden
        />
    </div>
</template>
<script>
export default {
    data() {
        return {
            files: false,
            edit: false,
            edit_content: '',
            remote_upl: '',
            remote_url: '',
            bad_file: false,
            saving: false,
            rename: false,
            selected: [],
            move_selected: [],
            errors: {},
            selall: false,
            del: false,
            create: false,
            itemname: '',
            itemtype: 'file',
            extract: false,
            move: false,
            copy: false,
            rename: false,
            new_name: '',
            old_name: '',
            edit_permissions: '',
            new_permissions: '',
            web_root: '',
            website_name: ''
        };
    },
    created() {
        this.getWebsite();
        this.EventBus.$on('doSearch', this.getFiles);
    },
    beforeDestroy() {
        this.EventBus.$off('doSearch', this.getFiles);
    },
    methods: {
        browseSegment(idx) {
            if(idx < 4) {
                return;
            }
            var path = '';
            for(var i = 0; i < this.files.segments.length; i++) {
                if(i <= idx) {
                    path += `/${this.files.segments[i][1]}`;
                }
            }
            this.$store.commit('setPath', path);
            this.getFiles();
        },
        getWebsite() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios.get(`/websites/${_this.$route.params.id}/`).then((res) => {
                _this.$store.commit('setBusy', false);
                if(res.data && res.data.metadata.path) {
                    _this.web_root = res.data.metadata.path;
                    _this.website_name = res.data.label;
                    _this.$store.commit('setPath', res.data.metadata.path);
                    _this.getFiles();
                } else {
                    toastr.error('Website root path cannot be obtained.');
                }
            }).catch((err) => {
                _this.$store.commit('setBusy', false);
                toastr.error('Files listing cannot be obtained.');
            });
        },
        chooseFiles() {
            document.getElementById('fileUpload').click();
        },
        clearSelected() {
            this.selected = [];
            this.selall = false;
        },
        editFile(file) {
            let _this = this;
            _this.edit = file;
            _this.bad_file = false;
            axios
                .get(`/file-manager/file-manipulation/?path=${file.path}`)
                .then((res) => {
                    _this.edit_content = res.data.content;
                })
                .catch((err) => {
                    _this.bad_file = true;
                    _this.edit_content = 'File manager cannot edit this file.';
                });
        },
        saveFile() {
            let _this = this;
            let fd = new FormData();
            fd.append('content', _this.edit_content);
            fd.append('path', _this.edit.path);
            axios
                .put(`/file-manager/file-manipulation/`, fd)
                .then((res) => {
                    toastr.success('File content has been updated.');
                    _this.saving = false;
                })
                .catch((err) => {
                    toastr.error('Error occured. File cannot be saved.');
                    _this.saving = false;
                });
        },
        getFiles(page = 1) {
            if (page == null) {
                return;
            }
            let search = document.getElementById('search-input').value;
            let _this = this;
            _this.$store.commit('setBusy', true);
            var path =_this.$store.state.path;
            if (path != null) { 
                path = encodeURIComponent(path);
            } else {
                return;
            }
            axios
                .get(`/file-manager/files/?page=${page}&search=${search}&path=${path}`)
                .then((res) => {
                    _this.files = res.data;
                    _this.$store.commit('setBusy', false);
                })
                .catch((err) => {
                    toastr.error('Directory listing cannot be retrieved.');
                    _this.$store.commit('setBusy', false);
                });
        },
        browseFile(file) {
            if (file.file_type == 'directory') {
                this.$store.commit('setPath', file.path);
                this.getFiles();
            }
        },
        compressFiles() {
            let _this = this;
            if (_this.selected.length == 0) {
                return;
            }
            _this.$store.commit('setBusy', true);
            let fd = new FormData();
            fd.append('path', _this.$store.state.path);
            fd.append('paths', _this.selected);
            axios
                .post('/file-manager/generate-archive/', fd)
                .then((res) => {
                    toastr.info('The archive for selected files has been generated.');
                    _this.$store.commit('setBusy', false);
                    _this.getFiles();
                    _this.clearSelected();
                })
                .catch((err) => {
                    toastr.error('Something went wrong. Archive cannot be generated.');
                    _this.$store.commit('setBusy', false);
                });
        },
        goBack() {
            let _this = this;
            let path = _this.$store.state.path;
            _this.selected = [];
            _this.selall = false;
            if (path == _this.web_root) {
                return;
            } else {
                let new_path = path.substring(0, path.lastIndexOf('/'));
                _this.$store.commit('setPath', new_path);
                _this.getFiles();
            }
        },
        goHome() {
            let path = this.$store.state.path;
            if (path == this.web_root) {
                return;
            }
            this.$store.commit('setPath', this.web_root);
            this.getFiles();
            this.clearSelected();
        },
        selectItem(path) {
            if (this.selected.includes(path)) {
                this.selected.splice(this.selected.indexOf(path), 1);
            } else {
                this.selected.push(path);
            }
        },
        createItem() {
            let _this = this;
            let fd = new FormData();
            fd.append('item_name', _this.itemname);
            fd.append('item_type', _this.itemtype);
            fd.append('path', _this.$store.state.path);
            _this.$store.commit('setBusy', true);
            axios
                .post('/file-manager/file-manipulation/', fd)
                .then((res) => {
                    _this.$store.commit('setBusy', false);
                    toastr.success('Iteam has been created successfully.');
                    _this.create = false;
                    _this.itemname = '';
                    _this.itemtype = 'file';
                    _this.getFiles();
                })
                .catch((err) => {
                    if(err.response && err.response.data.error) {
                        toastr.error(err.response.data.error);
                    } else {
                        toastr.error('Item with this name cannot be created.');
                    }
                    _this.$store.commit('setBusy', false);
                });
        },
        deleteItems() {
            let _this = this;
            if (_this.selected.length == 0) {
                _this.del = false;
                return;
            }
            _this.$store.commit('setBusy', true);
            let fd = new FormData();
            fd.append('paths', _this.selected);
            axios
                .post('/file-manager/delete-items/', fd)
                .then((res) => {
                    toastr.info('The selected items have been deleted.');
                    _this.$store.commit('setBusy', false);
                    _this.del = false;
                    _this.getFiles();
                    _this.clearSelected();
                })
                .catch((err) => {
                    toastr.error('Something went wrong. Files cannot be deleted.');
                    _this.$store.commit('setBusy', false);
                });
        },
        isZip(ids) {
            if (!ids) {
                return false;
            } else if (ids.length != 1) {
                return false;
            } else {
                let path = ids[0];
                var file = false;
                if (this.files && this.files.results.length) {
                    for (var i = 0; i < this.files.results.length; i++) {
                        if (path == this.files.results[i].path) {
                            file = this.files.results[i];
                        }
                    }
                }
                if (file && file.name.slice(-3) == 'zip') {
                    return true;
                }
            }
        },
        extractArchive() {
            let _this = this;
            let path = _this.selected[0];
            if (!path) {
                return;
            }
            _this.$store.commit('setBusy', true);
            let fd = new FormData();
            fd.append('path', path);
            fd.append('root_path', _this.$store.state.path);
            axios
                .post('/file-manager/extract-archive/', fd)
                .then((res) => {
                    toastr.info('The archive contents have been extracted.');
                    _this.$store.commit('setBusy', false);
                    _this.extract = false;
                    _this.getFiles();
                    _this.clearSelected();
                })
                .catch((err) => {
                    toastr.error('Something went wrong. Archive cannot be extracted.');
                    _this.$store.commit('setBusy', false);
                });
        },
        handleFileUpload() {
            let _this = this;
            let file = this.$refs.file.files[0];
            let fd = new FormData();
            fd.append('file', file);
            fd.append('path', _this.$store.state.path);
            _this.$store.commit('setBusy', true);
            axios
                .post('/file-manager/upload-files/', fd, {
                    headers: {
                        'Content-Type': 'multipart/form-data',
                    },
                })
                .then((res) => {
                    _this.$store.commit('setBusy', false);
                    toastr.success('Files have been uploaded successfully.');
                    _this.getFiles();
                }).catch((err) => {
                    _this.$store.commit('setBusy', false);
                    if(err.response && err.response.data.error) {
                        toastr.error(err.response.data.error);
                    } else {
                        toastr.error('File upload failed. Maybe it already exists.');
                    }
                });
        },
        validDestination() {
            let _this = this;
            if(!_this.move_selected.includes(_this.$store.state.path)) {
                return true;
            }
            return false;
        },
        moveItems() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            let fd = new FormData();
            if(_this.copy) {
                var action = 'copy';
            } else {
                var action = 'move';
            }
            fd.append('path', _this.$store.state.path);
            fd.append('paths', _this.move_selected);
            fd.append('action', action);
            axios.post('/file-manager/move-items/', fd).then((res) => {
                _this.$store.commit('setBusy', false);
                if(_this.copy) {
                    var action_taken = 'copied';
                } else {
                    var action_taken = 'moved';
                }
                toastr.success(`Items have been ${action_taken} successfully.`);
                _this.move = false;
                _this.copy = false;
                _this.selected = [];
                _this.move_selected = [];
                _this.getFiles();
            }).catch((err) => {
                toastr.error('An error occured while moving some items.');
                _this.$store.commit('setBusy', false);
            });
        },
        prepareRename() {
            if(this.selected.length != 1) {
                return;
            }
            this.rename = this.selected[0];
            for(var i = 0; i < this.files.results.length; i ++) {
                if(this.files.results[i].path == this.rename) {
                    this.old_name = this.files.results[i].name;
                    this.new_name = this.old_name;
                }
            }
        },
        renameItem() {
            let _this = this;
            let fd = new FormData();
            fd.append('path', _this.$store.state.path);
            fd.append('new_name', _this.new_name);
            fd.append('old_name', _this.old_name);
            _this.$store.commit('setBusy', true);
            axios.post('/file-manager/rename-item/', fd).then((res) => {
                toastr.success('The item has been renamed successfully.');
                _this.getFiles();
                _this.rename = false;
                _this.new_name = '';
                _this.old_name = '';
                _this.$store.commit('setBusy', false);
            }).catch((err) => {
                toastr.error('Error occured, the item cannot be renamed.');
                _this.$store.commit('setBusy', false);
            });
        },
        updatePermissions() {
            let _this = this;
            let fd = new FormData()
            fd.append('path', _this.edit_permissions);
            fd.append('permissions', _this.new_permissions);
            axios.post('/file-manager/update-permissions/', fd).then((res) => {
                _this.new_permissions = '';
                _this.edit_permissions = '';
                _this.getFiles();
                toastr.success('Permissions have been successfully updated.');
            }).catch((err) => {
                toastr.error('Permissions cannot be updated for this item.');
            });
        },
        fetchRemote() {
            let _this = this;
            _this.errors = {};
            let fd = new FormData();
            fd.append('path', _this.$store.state.path);
            fd.append('remote_url', _this.remote_url);
            _this.$store.commit('setBusy', true);
            axios.post('/file-manager/remote-fetch/', fd).then((res) => {
                _this.$store.commit('setBusy', false);
                _this.remote_url = '';
                _this.remote_upl = false;
                toastr.success('Remote file has been successfully downloaded.');
                _this.getFiles();
            }).catch((err) => {
                toastr.error('Remote file cannot be downloaded.');
                if(err.response && err.response.data) {
                    _this.errors = err.response.data;
                }
                _this.$store.commit('setBusy', false);
            });
        }
    },
    watch: {
        selall(newval, old) {
            let paths = [];
            if (newval == true) {
                if (this.files && this.files.results.length) {
                    for (var i = 0; i < this.files.results.length; i++) {
                        paths.push(this.files.results[i].path);
                    }
                }
            }
            this.selected = paths;
        }
    },
};
</script>
<style scoped>
tbody tr {
    cursor: pointer;
}
tbody tr:hover {
    background-color: #ebfaff !important;
}
.file-toolbar {
    font-size: 15px;
}
.file-toolbar a {
    text-decoration: none;
}
select {
    height: 30px;
}
</style>
