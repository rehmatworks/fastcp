from django import forms
from django.contrib.auth.models import User
from .utils.auth import do_login


class LoginForm(forms.Form):
    """Custom login form.
    
    We are not going to use the default login form of Django, because
    we will be authenticating the user sessions using SSH login info.
    """
    username = forms.CharField(label='SSH username')
    password = forms.CharField(widget=forms.PasswordInput())
    
    def clean(self):
        """Validate login info."""
        data = self.cleaned_data
        username = data.get('username')
        password = data.get('password')
        if username and password:
            user = User.objects.filter(username=username).first()
            if user:
                # login = do_login(username, password)
                # To-do: Crucial!!!! Need to validate the login. Currently
                # It just checks the username existence in Django
                # DB to authenticate a session.
                login = True
                if not login:
                    self.add_error('username', 'The provided login details are invalid.')
            else:
                self.add_error('username', 'The provided login details are invalid.')
        return data