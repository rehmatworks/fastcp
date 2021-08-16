let mix = require('laravel-mix');

mix.override((config) => {
    delete config.watchOptions;
});

mix.js('resources/js/fastcp.js', 'static/assets/js').vue({ version: 2 });