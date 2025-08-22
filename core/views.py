from django.shortcuts import render, redirect
from django.contrib.auth.decorators import login_required, user_passes_test
import logging
from .forms import LoginForm
from django.contrib.auth import login, logout
from .models import User
from django.http import FileResponse, Http404
from django.conf import settings
import os


@user_passes_test(
    lambda user: not user.is_authenticated,
    login_url='/',
    redirect_field_name='next',
)
def sign_in(request):
    """Custom login.

    We are going to validate the SSH login info of the user and then we will
    authenticate their session.
    """
    form = LoginForm()
    if request.method == 'POST':
        form = LoginForm(request.POST)
        # Debugging: log form validity and errors to help diagnose login issues
        is_valid = form.is_valid()
        # Avoid printing passwords; print username and errors only
        if is_valid or 'username' in request.POST:
            username = form.cleaned_data.get('username')
        else:
            username = request.POST.get('username')
        logger = logging.getLogger(__name__)
        logger.warning(
            "[sign_in debug] POST username=%r valid=%s",
            username,
            is_valid,
        )
        if form.errors:
            logger.warning("[sign_in debug] form.errors=%s", form.errors)
        if is_valid:
            user = User.objects.filter(username=username).first()
            login(request, user)
            logger.warning("[sign_in debug] login successful for %r", username)
            return redirect('/dashboard')
        else:
            logger.warning("[sign_in debug] login failed for %r", username)
    context = {'form': form}
    return render(request, 'registration/login.html', context=context)


def sign_out(request):
    logout(request)
    return redirect('/dashboard')


@login_required
def download_file(request):
    path = request.GET.get('path')
    user = request.user
    if user.is_superuser:
        BASE_PATH = settings.FILE_MANAGER_ROOT
    else:
        BASE_PATH = os.path.join(settings.FILE_MANAGER_ROOT, user.username)

    if path and path.startswith(BASE_PATH) and os.path.exists(path):
        response = FileResponse(open(path, 'rb'))
        return response
    raise Http404
